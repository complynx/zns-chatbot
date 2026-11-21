package task

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/passcopy"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type passQueueNoticeArgs struct {
	PassID  uuid.UUID `json:"pass_id"`
	Version int64     `json:"version"`
}

func (passQueueNoticeArgs) Kind() string { return "zinusya_pass_assignment_notice" }

type passQueueNoticeWorker struct {
	river.WorkerDefaults[passQueueNoticeArgs]

	service *Service
}

// Allocation has its own loop so a long queue scan cannot delay instance-lock checks or notifications.
func (s *Service) watchPassAllocation(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var cursor uuid.UUID
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := s.store.AdvancePassQueues(ctx, s.notificationScope(), cursor)
			cursor = next
			if err != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "pass queue allocation unavailable", "class", "pass_queue")
			}
		}
	}
}

func (s *Service) repairPassAssignmentNotices(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	jobs, err := s.store.ReadyPassAssignmentNotices(batch, s.notificationScope(), s.passNoticeCursor)
	if err != nil {
		return
	}
	for _, job := range jobs {
		_, err = s.queue.Insert(
			batch,
			passQueueNoticeArgs{PassID: job.ID, Version: job.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
		)
		if err != nil {
			return
		}
		s.passNoticeCursor = job.ID
	}
	if len(jobs) < db.NotificationRepairPageSize {
		s.passNoticeCursor = uuid.Nil
	}
}

func (w *passQueueNoticeWorker) Work(ctx context.Context, j *river.Job[passQueueNoticeArgs]) error {
	s := w.service
	scope := s.notificationScope()
	job := db.NotificationJob{ID: j.Args.PassID, Version: j.Args.Version}
	n, err := s.store.PassAssignmentNotice(ctx, scope, job)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	user, err := s.store.GetUser(ctx, n.UserID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, user)
	}
	if notificationDenied(err) {
		return s.store.FinishPassAssignmentNotice(ctx, job, "delivery_blocked", nil, 0)
	}
	if err != nil {
		return s.notificationRetry(err)
	}
	language, err := s.notificationLanguage(ctx, user)
	if err != nil {
		return err
	}
	zone, err := time.LoadLocation(n.Zone)
	if err != nil {
		return err
	}
	message := passAssignmentText(language, n.Event, n.Role, zone, n.Pass)
	reserved, err := s.store.ReservePassAssignmentNotice(
		ctx,
		db.PaymentScope{EventScope: scope, UserID: user.ID, Subject: user.Subject},
		job,
		n.EventID,
	)
	if err != nil {
		return err
	}
	if !reserved {
		return s.store.FinishPassAssignmentNotice(ctx, job, "delivery_blocked", nil, 0)
	}
	return s.sendBusinessNotification(
		ctx,
		telegram.SendMessageRequest{ChatID: user.TelegramUserID, Text: message},
		"pass_assignment",
		func(finish context.Context, state string, receipt json.RawMessage, delay time.Duration) error {
			return s.store.FinishPassAssignmentNotice(finish, job, state, receipt, delay)
		},
	)
}

func passAssignmentText(language, event, role string, zone *time.Location, pass registrations.Pass) string {
	role = structuredRole(role, language == "ru")
	if pass.PriceMinor == 0 {
		return passcopy.Text(language, "queue.assignedFree", event, role, registrationsCommand)
	}
	amount := noticeAmount(pass.PriceMinor, pass.CurrencyScale, pass.Currency)
	format := func(t time.Time) string { return t.In(zone).Format("2006-01-02 15:04 MST") }
	due := pass.PaymentDueAt
	if deposit := pass.Deposit; deposit != nil {
		if deposit.RequiredMinor == 0 {
			due = deposit.FinalDueAt
		} else if deposit.RequiredMinor < pass.PriceMinor {
			minimum := noticeAmount(deposit.RequiredMinor, pass.CurrencyScale, pass.Currency)
			return passcopy.Text(
				language,
				"queue.assignedDeposit",
				event,
				role,
				amount,
				minimum,
				format(due),
				format(deposit.FinalDueAt),
				paymentsCommand,
			)
		}
	}
	return passcopy.Text(language, "queue.assigned", event, role, amount, format(due), paymentsCommand)
}
