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

type orderNotificationArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (orderNotificationArgs) Kind() string { return "zinusya_order_notification" }

type orderNotificationWorker struct {
	river.WorkerDefaults[orderNotificationArgs]

	service *Service
}

func (w *orderNotificationWorker) Work(ctx context.Context, job *river.Job[orderNotificationArgs]) error {
	started := time.Now()
	err := w.service.notifyOrder(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) repairOrderNotifications(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.scheduleOrderNotifications(batch); err != nil && ctx.Err() == nil {
		s.record(ctx, "order_notifications", metricError)
		s.logger.WarnContext(ctx, "order notification scheduling unavailable", "class", "order_notification_queue")
	}
}

func (s *Service) scheduleOrderNotifications(ctx context.Context) error {
	jobs, err := s.store.ReadyOrderNotifications(ctx, s.notificationScope(), s.orderNotificationCursor)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, err = s.queue.Insert(ctx, orderNotificationArgs{NotificationID: job.ID, Version: job.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil {
			return err
		}
		s.orderNotificationCursor = job.ID
	}
	if len(jobs) < db.NotificationRepairPageSize {
		s.orderNotificationCursor = uuid.Nil
	}
	return nil
}

func (s *Service) notifyOrder(ctx context.Context, args orderNotificationArgs) error {
	scope := s.notificationScope()
	n, err := s.store.OrderNotification(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishOrderNotification(ctx, n, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, _, err := s.store.OrderNotificationRecipient(ctx, scope, n.ID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.store.BlockOrderNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if notificationDenied(err) {
		return s.store.BlockOrderNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	// IAM/profile IO can wait. Refresh eligibility before building audience-specific text.
	u, reviewer, err := s.store.OrderNotificationRecipient(ctx, scope, n.ID)
	if notificationDenied(err) {
		return s.store.BlockOrderNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	text, err := orderNotificationText(n, language, reviewer)
	if err != nil {
		return err
	}
	input := telegram.SendMessageRequest{ChatID: u.TelegramUserID, Text: text}
	if reviewer && s.options.MiniAppURL != "" {
		input.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{
					Text: structuredText(language == "ru", "Review order proof", "Проверить оплату заказа"),
					WebApp: &telegram.WebAppInfo{
						URL: s.options.MiniAppURL + "#order-review?event=" + n.Evidence.EventID.String() + "&evidence=" + n.EvidenceID.String(),
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
	reserved, err := s.store.ReserveOrderNotification(ctx, principal, n.ID, n.Version, payload)
	if notificationDenied(err) {
		return s.store.BlockOrderNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	return s.sendBusinessNotification(
		ctx,
		input,
		"order_notifications",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishOrderNotification(finishCtx, reserved, state, raw, delay)
		},
	)
}
