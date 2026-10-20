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
	"github.com/complynx/zns-chatbot/internal/massage"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type massageReminderArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (massageReminderArgs) Kind() string { return "zinusya_massage_reminder" }

type massageReminderWorker struct {
	river.WorkerDefaults[massageReminderArgs]

	service *Service
}

func (w *massageReminderWorker) Work(ctx context.Context, job *river.Job[massageReminderArgs]) error {
	started := time.Now()
	err := w.service.notifyMassage(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) repairMassageReminders(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	rows, err := s.store.ReadyMassageReminders(batch, s.notificationScope(), s.massageReminderCursor)
	if err == nil {
		for _, n := range rows {
			if _, err = s.queue.Insert(
				batch,
				massageReminderArgs{NotificationID: n.ID, Version: n.Version},
				&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
			); err != nil {
				break
			}
			s.massageReminderCursor = n.ID
		}
		if err == nil && len(rows) < db.NotificationRepairPageSize {
			s.massageReminderCursor = uuid.Nil
		}
	}
	if err != nil && ctx.Err() == nil {
		s.record(ctx, "massage_reminders", metricError)
		s.logger.WarnContext(ctx, "massage reminder scheduling unavailable", "class", "massage_reminder_queue")
	}
}

func (s *Service) notifyMassage(ctx context.Context, args massageReminderArgs) error {
	scope := s.notificationScope()
	n, err := s.store.MassageReminder(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishMassageReminder(ctx, n, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.MassageReminderRecipient(ctx, scope, n.ID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.store.BlockMassageReminder(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if notificationDenied(err) {
		return s.store.BlockMassageReminder(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	u, err = s.store.MassageReminderRecipient(ctx, scope, n.ID)
	if notificationDenied(err) {
		return s.store.BlockMassageReminder(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	text, err := massageReminderText(n, language)
	if err != nil {
		return err
	}
	input := telegram.SendMessageRequest{ChatID: u.TelegramUserID, Text: text}
	if s.options.MiniAppURL != "" {
		input.ReplyMarkup = &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{
					{
						Text:   structuredText(language == "ru", "Open massage bookings", "Открыть записи на массаж"),
						WebApp: &telegram.WebAppInfo{URL: s.options.MiniAppURL + "#massage"},
					},
				},
			},
		}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	reserved, err := s.store.ReserveMassageReminder(
		ctx,
		db.PaymentScope{EventScope: scope, UserID: u.ID, Subject: u.Subject},
		n.ID,
		n.Version,
		payload,
	)
	if errors.Is(err, db.ErrConflict) {
		return nil
	}
	if notificationDenied(err) {
		return s.store.BlockMassageReminder(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	return s.sendBusinessNotification(
		ctx,
		input,
		"massage_reminders",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishMassageReminder(finishCtx, reserved, state, raw, delay)
		},
	)
}

// Absolute event-local time remains factual for short-notice and late catch-up.
// Reminder kinds label scheduled stages, not the time remaining at delivery.
// Labels are plain text; a displayed price does not assert confirmed payment.
func massageReminderText(n db.MassageReminder, language string) (string, error) {
	terms := n.Booking.Snapshot.Terms
	zone, err := time.LoadLocation(terms.Timezone)
	if err != nil {
		return "", err
	}
	if terms.CurrencyScale < 0 || terms.CurrencyScale > massage.MaxCurrencyScale {
		return "", errors.New("invalid massage reminder currency scale")
	}
	specialist, service := terms.SpecialistName.EN, terms.ServiceName.EN
	heading, startLabel, serviceLabel, specialistLabel, priceLabel, referenceLabel := "Massage booking reminder", "Start", "Service", "Specialist", "Price", "Booking"
	advance, final := " (advance)", " (final)"
	if language == "ru" {
		specialist, service = terms.SpecialistName.RU, terms.ServiceName.RU
		heading, startLabel, serviceLabel, specialistLabel, priceLabel, referenceLabel = "Напоминание о записи на массаж", "Начало", "Услуга", "Специалист", "Стоимость", "Запись"
		advance, final = " (предварительное)", " (заключительное)"
	}
	switch n.Kind {
	case db.MassageReminderLong:
		heading += advance
	case db.MassageReminderShort:
		heading += final
	}
	return fmt.Sprintf(
		"%s\n%s\n%s: %s (%s)\n%s: %s\n%s: %s\n%s: %s\n%s: %s",
		heading,
		n.Booking.Snapshot.EventName,
		startLabel,
		terms.StartsAt.In(zone).Format(time.RFC3339),
		terms.Timezone,
		serviceLabel,
		service,
		specialistLabel,
		specialist,
		priceLabel,
		noticeAmount(terms.PriceMinor, int32(terms.CurrencyScale), terms.Currency),
		referenceLabel,
		n.Booking.ID,
	), nil
}
