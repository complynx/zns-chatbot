package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	pairNoticePending  = "pending"
	pairNoticeAccepted = "accepted"
)

type pairNotificationArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (pairNotificationArgs) Kind() string { return "zinusya_pair_notification" }

type pairNotificationWorker struct {
	river.WorkerDefaults[pairNotificationArgs]

	service *Service
}

func (w *pairNotificationWorker) Work(ctx context.Context, job *river.Job[pairNotificationArgs]) error {
	started := time.Now()
	err := w.service.notifyPair(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) schedulePairNotifications(ctx context.Context) error {
	rows, err := s.store.ReadyPairInvitationNotifications(ctx, s.notificationScope(), s.pairNotificationCursor)
	if err != nil {
		return err
	}
	for _, row := range rows {
		_, err = s.queue.Insert(ctx, pairNotificationArgs{NotificationID: row.ID, Version: row.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}})
		if err != nil {
			return err
		}
		s.pairNotificationCursor = row.ID
	}
	if len(rows) < db.NotificationRepairPageSize {
		s.pairNotificationCursor = uuid.Nil
	}
	return nil
}

func (s *Service) repairPairNotifications(ctx context.Context) {
	batchCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.schedulePairNotifications(batchCtx); err != nil && ctx.Err() == nil {
		s.record(ctx, "pair_notifications", metricError)
		s.logger.WarnContext(ctx, "pair notification scheduling unavailable", "class", "pair_notification_queue")
	}
}

func pairNoticeDenied(err error) bool {
	return errors.Is(err, db.ErrPairDenied) || notificationDenied(err)
}

func (s *Service) notifyPair(ctx context.Context, args pairNotificationArgs) error {
	scope := s.notificationScope()
	n, err := s.store.PairInvitationNotification(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishPairNotification(ctx, n.PairInvitationNotification, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != pairNoticePending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.PairNotificationRecipient(ctx, scope, n.PairInvitationNotification)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if pairNoticeDenied(err) {
		return s.store.BlockPairNotification(ctx, n.PairInvitationNotification)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if pairNoticeDenied(err) {
		return s.store.BlockPairNotification(ctx, n.PairInvitationNotification)
	}
	if err != nil {
		return err
	}
	text, err := pairNotificationText(n, language)
	if err != nil {
		return err
	}
	input := telegram.SendMessageRequest{ChatID: u.TelegramUserID, Text: text}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	principal := db.PaymentScope{EventScope: scope, UserID: u.ID, Subject: u.Subject}
	reserved, err := s.store.ReservePairNotification(ctx, principal, n, payload)
	if pairNoticeDenied(err) {
		return s.store.BlockPairNotification(ctx, n.PairInvitationNotification)
	}
	if err != nil {
		return err
	}
	return s.sendPairNotification(ctx, reserved, input)
}

func (s *Service) sendPairNotification(
	ctx context.Context,
	reserved db.PairInvitationNotification,
	input telegram.SendMessageRequest,
) error {
	return s.sendBusinessNotification(
		ctx,
		input,
		"pair_notifications",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishPairNotification(finishCtx, reserved, state, raw, delay)
		},
	)
}

// Plain text keeps names and event labels out of Markdown and agent instructions.
func pairNotificationText(n db.PairInvitationDelivery, language string) (string, error) {
	v := n.Context
	zone, err := time.LoadLocation(v.Timezone)
	if err != nil {
		return "", err
	}
	reference := fmt.Sprintf("\nEvent: %.200s (%s)\nInvitation: %s", v.EventName, v.Invitation.EventID, n.InvitationID)
	if language == "ru" {
		reference = fmt.Sprintf(
			"\nЭвент: %.200s (%s)\nПриглашение: %s",
			v.EventName,
			v.Invitation.EventID,
			n.InvitationID,
		)
	}
	if n.Kind == pairNoticePending {
		expires := v.Invitation.ExpiresAt.In(zone).Format(time.RFC3339)
		if language == "ru" {
			return fmt.Sprintf(
				"%.200s (%s) приглашает вас в пару, роль: %s.\nСрок: %s (%s).\nЧтобы принять, явно подтвердите приглашение боту. Принятие добавляет в очередь пары; пасс ещё не назначен.%s",
				v.InviterName,
				v.InviterUserID,
				v.RequiredRole,
				expires,
				v.Timezone,
				reference,
			), nil
		}
		return fmt.Sprintf(
			"%.200s (%s) invites you to pair as %s.\nExpires: %s (%s).\nTo accept, explicitly confirm this invitation to the bot. Acceptance joins the pair waitlist; no pass is assigned yet.%s",
			v.InviterName,
			v.InviterUserID,
			v.RequiredRole,
			expires,
			v.Timezone,
			reference,
		), nil
	}
	texts := map[string]string{
		pairNoticeAccepted: "Your invitation was accepted. The pair joined the waitlist; no passes were assigned by acceptance.",
		"declined":         "Your invitation was declined. The invitation's allocation hold has been released.",
		"revoked":          "The inviter revoked this invitation. It can no longer be accepted.",
		"expired":          "Your invitation expired. The invitation's allocation hold has been released.",
	}
	if language == "ru" {
		texts = map[string]string{
			pairNoticeAccepted: "Ваше приглашение принято. Пара добавлена в очередь; принятие не назначает пассы.",
			"declined":         "Ваше приглашение отклонено. Удержание регистрации этим приглашением снято.",
			"revoked":          "Приглашающий отозвал приглашение. Принять его больше нельзя.",
			"expired":          "Срок вашего приглашения истёк. Удержание регистрации этим приглашением снято.",
		}
	}
	text, ok := texts[n.Kind]
	if !ok {
		return "", errors.New("invalid pair notification kind")
	}
	return text + reference, nil
}
