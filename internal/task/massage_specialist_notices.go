package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const massageSpecialistSoon = "soon"

type massageSpecialistNoticeArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (massageSpecialistNoticeArgs) Kind() string { return "zinusya_massage_specialist_notice" }

type massageSpecialistNoticeWorker struct {
	river.WorkerDefaults[massageSpecialistNoticeArgs]

	service *Service
}

func (w *massageSpecialistNoticeWorker) Work(ctx context.Context, job *river.Job[massageSpecialistNoticeArgs]) error {
	started := time.Now()
	err := w.service.notifyMassageSpecialist(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) repairMassageSpecialistNotices(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err := s.scheduleMassageSpecialistNotices(batch)
	if err != nil && ctx.Err() == nil {
		s.record(ctx, "massage_specialist_notices", metricError)
		s.logger.WarnContext(
			ctx,
			"massage specialist notification scheduling unavailable",
			"class",
			"massage_specialist_notice_queue",
		)
	}
}

func (s *Service) scheduleMassageSpecialistNotices(ctx context.Context) error {
	if err := s.store.CreateDueMassageSpecialistNotifications(ctx, s.notificationScope()); err != nil {
		return err
	}
	rows, err := s.store.ReadyMassageSpecialistNotifications(
		ctx,
		s.notificationScope(),
		s.massageSpecialistNoticeCursor,
	)
	if err != nil {
		return err
	}
	for _, n := range rows {
		if _, err = s.queue.Insert(
			ctx,
			massageSpecialistNoticeArgs{NotificationID: n.ID, Version: n.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
		); err != nil {
			return err
		}
		s.massageSpecialistNoticeCursor = n.ID
	}
	if len(rows) < db.NotificationRepairPageSize {
		s.massageSpecialistNoticeCursor = uuid.Nil
	}
	return nil
}

func (s *Service) notifyMassageSpecialist(ctx context.Context, args massageSpecialistNoticeArgs) error {
	scope := s.notificationScope()
	n, err := s.store.MassageSpecialistNotification(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishMassageSpecialistNotification(ctx, n, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.MassageSpecialistNotificationRecipient(ctx, scope, n.ID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.store.BlockMassageSpecialistNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if notificationDenied(err) {
		return s.store.BlockMassageSpecialistNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	u, err = s.store.MassageSpecialistNotificationRecipient(ctx, scope, n.ID)
	if notificationDenied(err) {
		return s.store.BlockMassageSpecialistNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	input, err := s.massageSpecialistMessage(ctx, n, u, language)
	if notificationDenied(err) {
		return s.store.BlockMassageSpecialistNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	reserved, err := s.store.ReserveMassageSpecialistNotification(
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
		return s.store.BlockMassageSpecialistNotification(ctx, scope, n.ID, n.Version)
	}
	if err != nil {
		return err
	}
	return s.sendBusinessNotification(
		ctx,
		input,
		"massage_specialist_notices",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishMassageSpecialistNotification(finishCtx, reserved, state, raw, delay)
		},
	)
}

func (s *Service) massageSpecialistMessage(
	ctx context.Context,
	n db.MassageSpecialistNotification,
	u db.User,
	language string,
) (telegram.SendMessageRequest, error) {
	contact := ""
	if n.Kind == "booked" || n.Kind == massageSpecialistSoon {
		client, contactErr := s.store.MassageStaffClient(ctx, db.ProfileScope{
			BotID: u.BotID, Organization: u.Organization, UserID: u.ID, Subject: u.Subject,
		}, n.Booking.EventID, n.Booking.ID)
		if contactErr != nil {
			return telegram.SendMessageRequest{}, contactErr
		}
		contact = client.Contact
	}
	input, err := massageSpecialistNoticeMessage(n, language, contact)
	if err != nil {
		return telegram.SendMessageRequest{}, err
	}
	input.ChatID = u.TelegramUserID
	if s.options.MiniAppURL != "" {
		input.ReplyMarkup = &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{
					{
						Text: structuredText(
							language == "ru",
							"Open massage timetable",
							"Открыть расписание массажей",
						),
						WebApp: &telegram.WebAppInfo{URL: s.options.MiniAppURL + "#massage-staff"},
					},
				},
			},
		}
	}
	return input, nil
}

// Plain text keeps client and event names as data. Only a current own-booking
// contact can become an entity, and cancelled notices never include contacts.
func massageSpecialistNoticeMessage(
	n db.MassageSpecialistNotification,
	language, contact string,
) (telegram.SendMessageRequest, error) {
	terms := n.Booking.Snapshot.Terms
	zone, err := time.LoadLocation(terms.Timezone)
	if err != nil {
		return telegram.SendMessageRequest{}, err
	}
	if terms.CurrencyScale < 0 || terms.CurrencyScale > massage.MaxCurrencyScale {
		return telegram.SendMessageRequest{}, errors.New("invalid massage specialist notification currency scale")
	}
	heading, startLabel, clientLabel, serviceLabel, priceLabel, referenceLabel, contactLabel := "New massage booking", "Start", "Client", "Service", "Price", "Booking", "Contact client"
	service, specialist := terms.ServiceName.EN, terms.SpecialistName.EN
	specialistLabel, endLabel := "Specialist", "End"
	switch n.Kind {
	case stateCancelled:
		heading = "Massage booking cancelled"
	case massageSpecialistSoon:
		heading = "Massage session reminder"
	}
	if language == "ru" {
		heading, startLabel, clientLabel, serviceLabel, priceLabel, referenceLabel, contactLabel = "Новая запись на массаж", "Начало", "Клиент", "Услуга", "Стоимость", "Запись", "Связаться с клиентом"
		service, specialist = terms.ServiceName.RU, terms.SpecialistName.RU
		specialistLabel, endLabel = "Специалист", "Окончание"
		switch n.Kind {
		case stateCancelled:
			heading = "Запись на массаж отменена"
		case massageSpecialistSoon:
			heading = "Напоминание о сессии массажа"
		}
	}
	text := fmt.Sprintf(
		"%s\n%s\n%s: %s (%s)\n%s: %s\n%s: %s\n%s: %s\n%s: %s",
		heading,
		n.Booking.Snapshot.EventName,
		startLabel,
		terms.StartsAt.In(zone).Format(time.RFC3339),
		terms.Timezone,
		clientLabel,
		n.ClientName,
		serviceLabel,
		service,
		priceLabel,
		noticeAmount(terms.PriceMinor, int32(terms.CurrencyScale), terms.Currency),
		referenceLabel,
		n.Booking.ID,
	)
	text += fmt.Sprintf(
		"\n%s: %s\n%s: %s (%s)",
		specialistLabel,
		specialist,
		endLabel,
		terms.TreatmentEndsAt.In(zone).Format(time.RFC3339),
		terms.Timezone,
	)
	input := telegram.SendMessageRequest{Text: text}
	if (n.Kind == "booked" || n.Kind == massageSpecialistSoon) && contact != "" {
		input.Text += "\n" + contactLabel
		input.Entities = []telegram.MessageEntity{
			{
				Type:   "text_link",
				Offset: len(utf16.Encode([]rune(text + "\n"))),
				Length: len(utf16.Encode([]rune(contactLabel))),
				URL:    contact,
			},
		}
	}
	return input, nil
}
