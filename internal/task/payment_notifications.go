package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type notificationArgs struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Version        int64     `json:"version"`
}

func (notificationArgs) Kind() string { return "zinusya_payment_notification" }

type notificationWorker struct {
	river.WorkerDefaults[notificationArgs]

	service *Service
}

func (w *notificationWorker) Work(ctx context.Context, job *river.Job[notificationArgs]) error {
	started := time.Now()
	err := w.service.notifyPayment(ctx, job.Args)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}

func (s *Service) notificationScope() db.EventScope {
	bot, organization, _ := s.iam.DeploymentScope()
	return db.EventScope{BotID: bot, Organization: organization}
}

// Repair schedules committed business notices; River jobs contain only IDs/versions.
func (s *Service) scheduleNotifications(ctx context.Context) error {
	rows, err := s.store.ReadyPaymentNotifications(ctx, s.notificationScope(), s.notificationCursor)
	if err != nil {
		return err
	}
	for _, row := range rows {
		_, err = s.queue.Insert(
			ctx,
			notificationArgs{NotificationID: row.ID, Version: row.Version},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
		)
		if err != nil {
			return err
		}
		s.notificationCursor = row.ID
	}
	if len(rows) < db.NotificationRepairPageSize {
		s.notificationCursor = uuid.Nil
	}
	return nil
}

func notificationDenied(err error) bool {
	return errors.Is(err, payments.ErrDenied) || errors.Is(err, payments.ErrUnavailable) ||
		errors.Is(err, identity.ErrDenied) ||
		errors.Is(err, identity.ErrConflict) ||
		errors.Is(err, pgx.ErrNoRows)
}

func (s *Service) notifyPayment(ctx context.Context, args notificationArgs) error {
	scope := s.notificationScope()
	n, err := s.store.PaymentNotification(ctx, scope, args.NotificationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if n.State == deliverySending && n.Version == args.Version+1 {
		return s.store.FinishNotification(ctx, n, stateDeliveryUnknown, nil, 0)
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.NotificationRecipient(ctx, scope, n)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.store.BlockNotification(ctx, n)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	language, err := s.notificationLanguage(ctx, u)
	if notificationDenied(err) {
		return s.store.BlockNotification(ctx, n)
	}
	if err != nil {
		return err
	}
	text, err := paymentNotificationText(n, language)
	if err != nil {
		return err
	}
	input := telegram.SendMessageRequest{ChatID: u.TelegramUserID, Text: text}
	if n.Kind == "submitted" && s.options.MiniAppURL != "" {
		input.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{
					Text: structuredText(language == "ru", "Review proof", "Проверить подтверждение"),
					WebApp: &telegram.WebAppInfo{
						URL: s.options.MiniAppURL + "#payment-review?event_id=" + n.Document.Evidence.EventID.String() + "&evidence_id=" + n.EvidenceID.String(),
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
	reserved, err := s.store.ReserveNotification(ctx, principal, n, payload)
	if notificationDenied(err) {
		return s.store.BlockNotification(ctx, n)
	}
	if err != nil {
		return err
	}
	return s.sendNotification(ctx, reserved, input)
}

func (s *Service) sendNotification(
	ctx context.Context,
	reserved db.PaymentNotification,
	input telegram.SendMessageRequest,
) error {
	return s.sendBusinessNotification(
		ctx,
		input,
		"notifications",
		func(finishCtx context.Context, state string, raw json.RawMessage, delay time.Duration) error {
			return s.store.FinishNotification(finishCtx, reserved, state, raw, delay)
		},
	)
}

// sendBusinessNotification persists the Telegram outcome even during shutdown.
// Unknown delivery is never converted into an automatic resend.
func (s *Service) sendBusinessNotification(ctx context.Context, input telegram.SendMessageRequest,
	component string, finish func(context.Context, string, json.RawMessage, time.Duration) error,
) error {
	receipt, sendErr := s.tg.SendMessage(ctx, input)
	outcome, delay := deliveryOutcome(sendErr)
	if outcome == deliveryRetryable && delay <= 0 {
		delay = s.options.PollInterval
	}
	raw, _ := json.Marshal(map[string]int64{"message_id": receipt.MessageID, "chat_id": receipt.ChatID})
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := finish(finishCtx, outcome, raw, delay)
	metric := metricSuccess
	if err != nil || outcome == stateDeliveryUnknown || outcome == stateDeliveryFailed {
		metric = metricError
	} else if outcome == deliveryRetryable {
		metric = metricRetry
	}
	s.record(ctx, component, metric)
	return err
}

func (s *Service) verifyNotificationUser(ctx context.Context, u db.User) error {
	credential, err := s.iam.UserToken(ctx, u.Subject)
	if err != nil {
		return err
	}
	p, err := s.iam.Verify(ctx, credential.Token)
	if err != nil {
		return err
	}
	if p.Subject != u.Subject || p.OrganizationID != u.Organization {
		return identity.ErrDenied
	}
	return nil
}

func noticeAmount(amount int64, scale int32, currency string) string {
	divisor := int64(1)
	for range scale {
		divisor *= 10
	}
	value := strconv.FormatInt(amount, 10)
	if scale > 0 {
		value = fmt.Sprintf("%d.%0*d", amount/divisor, scale, amount%divisor)
	}
	return value + " " + currency
}

// Plain text treats event names and review reasons as data, not Markdown or instructions.
func paymentNotificationText(n db.PaymentNotification, language string) (string, error) {
	document := n.Document
	evidence := document.Evidence
	zone, err := time.LoadLocation(document.Timezone)
	if err != nil {
		return "", err
	}
	reference := fmt.Sprintf("\nEvent: %s\nEvidence: %s", evidence.EventID, evidence.ID)
	if language == "ru" {
		reference = fmt.Sprintf("\nЭвент: %s\nДоказательство: %s", evidence.EventID, evidence.ID)
	}
	if n.Kind == "submitted" {
		return submittedNotificationText(document, language, reference, zone), nil
	}
	if evidence.Review == nil || document.Balance == nil {
		return "", errors.New("payment decision notification is incomplete")
	}
	review := evidence.Review
	decisionTime := review.ReviewedAt.In(zone).Format(time.RFC3339)
	if n.Kind == payments.Rejected {
		if language == "ru" {
			return fmt.Sprintf(
				"Подтверждение оплаты отклонено: %s\nПричина: %s\nДеньги не начислены по этому доказательству.%s",
				document.EventName,
				review.Reason,
				reference,
			), nil
		}
		return fmt.Sprintf(
			"Payment evidence rejected: %s\nReason: %s\nThis evidence credited no money.%s",
			document.EventName,
			review.Reason,
			reference,
		), nil
	}
	amount := noticeAmount(review.AmountMinor, evidence.CurrencyScale, evidence.Currency)
	remaining := noticeAmount(document.Balance.RemainingMinor, evidence.CurrencyScale, evidence.Currency)
	status := "Pass is not fully paid."
	if document.Balance.DepositSatisfied {
		status = "Deposit requirement is satisfied; the pass is not fully paid."
	}
	if document.Balance.FullyPaid {
		status = "Pass is fully paid."
	}
	if language == "ru" {
		status = "Пасс ещё не оплачен полностью."
		if document.Balance.DepositSatisfied {
			status = "Предоплата подтверждена; пасс ещё не оплачен полностью."
		}
		if document.Balance.FullyPaid {
			status = "Пасс оплачен полностью."
		}
		return fmt.Sprintf(
			"Оплата подтверждена: %s\nПодтверждённая сумма: %s\nРешение: %s (%s)\nОсталось к оплате после этого решения: %s\nСостояние сразу после этого решения: %s\nПричина решения: %s%s",
			document.EventName,
			amount,
			decisionTime,
			document.Timezone,
			remaining,
			status,
			review.Reason,
			reference,
		), nil
	}
	return fmt.Sprintf(
		"Payment confirmed: %s\nConfirmed amount: %s\nDecision: %s (%s)\nRemaining after this decision: %s\nStatus immediately after this decision: %s\nDecision reason: %s%s",
		document.EventName,
		amount,
		decisionTime,
		document.Timezone,
		remaining,
		status,
		review.Reason,
		reference,
	), nil
}

func submittedNotificationText(document db.PaymentNotice, language, reference string, zone *time.Location) string {
	evidence := document.Evidence
	amount := noticeAmount(evidence.AmountMinor, evidence.CurrencyScale, evidence.Currency)
	sent := evidence.SentAt.In(zone).Format(time.RFC3339)
	due := evidence.DueAt.In(zone).Format(time.RFC3339)
	if language == "ru" {
		timely := "получено после дедлайна; требуется решение по случаю"
		if evidence.Timely {
			timely = "получено до дедлайна"
		}
		return fmt.Sprintf(
			"Новое подтверждение для проверки: %s\nЗаявленная сумма: %s\nОтправлено: %s\nДедлайн: %s (%s)\n%s. Оплата ещё не подтверждена.\nПопросите Зинусю показать доказательство по ID и подтвердите только после проверки.%s",
			document.EventName,
			amount,
			sent,
			due,
			document.Timezone,
			timely,
			reference,
		)
	}
	timely := "received after deadline; a case decision is required"
	if evidence.Timely {
		timely = "received before deadline"
	}
	return fmt.Sprintf(
		"Payment evidence to review: %s\nReported amount: %s\nSent: %s\nDeadline: %s (%s)\n%s. Payment is not confirmed.\nAsk Zinusya to show the evidence by ID and confirm only after review.%s",
		document.EventName,
		amount,
		sent,
		due,
		document.Timezone,
		timely,
		reference,
	)
}
