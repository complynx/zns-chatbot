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
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type knowledgeNotificationArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (knowledgeNotificationArgs) Kind() string { return "zinusya_knowledge_notification" }

type knowledgeNotificationWorker struct {
	river.WorkerDefaults[knowledgeNotificationArgs]

	service *Service
}

func (w *knowledgeNotificationWorker) Work(ctx context.Context, job *river.Job[knowledgeNotificationArgs]) error {
	started := time.Now()
	err := w.service.notifyKnowledge(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) repairKnowledgeNotifications(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.scheduleKnowledgeNotifications(batch); err != nil && ctx.Err() == nil {
		s.record(ctx, "knowledge_notifications", metricError)
		s.logger.WarnContext(ctx, "knowledge notification scheduling unavailable", "class", "order_notification_queue")
	}
}

func (s *Service) scheduleKnowledgeNotifications(ctx context.Context) error {
	if err := s.store.EnsureKnowledgeNotifications(ctx, s.notificationScope()); err != nil {
		return err
	}
	jobs, err := s.store.ReadyKnowledgeNotifications(ctx, s.notificationScope(), s.knowledgeNotificationCursor)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, err = s.queue.Insert(ctx, knowledgeNotificationArgs{NotificationID: job.ID, Version: job.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil {
			return err
		}
		s.knowledgeNotificationCursor = job.ID
	}
	if len(jobs) < db.NotificationRepairPageSize {
		s.knowledgeNotificationCursor = uuid.Nil
	}
	return nil
}

func (s *Service) notifyKnowledge(ctx context.Context, args knowledgeNotificationArgs) error {
	scope := s.notificationScope()
	n, err := s.store.KnowledgeNotification(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishKnowledgeNotification(ctx, n, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.KnowledgeNotificationRecipient(ctx, scope, n.ID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.store.BlockKnowledgeNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if notificationDenied(err) {
		return s.store.BlockKnowledgeNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	text, err := knowledgeNotificationText(n.Kind, language)
	if err != nil {
		return err
	}
	input := telegram.SendMessageRequest{ChatID: u.TelegramUserID, Text: text}
	if s.options.MiniAppURL != "" {
		input.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{
					Text: structuredText(language == "ru", "Open knowledge suggestion", "Открыть предложение"),
					WebApp: &telegram.WebAppInfo{
						URL: s.options.MiniAppURL + "#knowledge?suggestion=" + n.SuggestionID.String(),
					},
				},
			},
		}}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	principal := db.PaymentScope{EventScope: scope, UserID: u.ID, Subject: u.Subject}
	reserved, err := s.store.ReserveKnowledgeNotification(ctx, principal, n.ID, n.Version, payload)
	if notificationDenied(err) {
		return s.store.BlockKnowledgeNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	return s.sendBusinessNotification(
		ctx,
		input,
		"knowledge_notifications",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishKnowledgeNotification(finishCtx, reserved, state, raw, delay)
		},
	)
}

// Notification prose contains no untrusted title, proposed text or review reason.
func knowledgeNotificationText(kind, language string) (string, error) {
	ru := language == "ru"
	switch kind {
	case "pending":
		return structuredText(
			ru,
			"A knowledge suggestion awaits independent review.",
			"Предложение для базы знаний ожидает независимой проверки.",
		), nil
	case "approved":
		return structuredText(
			ru,
			"Your knowledge suggestion was approved by a moderator.",
			"Модератор одобрил твоё предложение для базы знаний.",
		), nil
	case "rejected":
		return structuredText(
			ru,
			"A moderator declined your knowledge suggestion. Open it to read the reason.",
			"Модератор отклонил твоё предложение для базы знаний. Причина указана в предложении.",
		), nil
	default:
		return "", errors.New("invalid knowledge notice kind")
	}
}
