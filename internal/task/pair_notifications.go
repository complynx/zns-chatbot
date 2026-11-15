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
		return s.notificationRetry(err)
	}
	language, err := s.notificationLanguage(ctx, u)
	if pairNoticeDenied(err) {
		return s.store.BlockPairNotification(ctx, n.PairInvitationNotification)
	}
	if err != nil {
		return err
	}
	var inviter telegram.User
	if n.Kind == pairNoticePending {
		person, personErr := s.store.GetUser(ctx, n.Context.InviterUserID)
		if personErr != nil {
			s.logger.WarnContext(ctx, "pair inviter mention unavailable", "class", "message_people")
		} else {
			inviter = pairInviterMention(person, scope, n.Context.InviterName)
		}
	}
	input, err := pairNotificationContent(n, language, inviter)
	if err != nil {
		return err
	}
	input.ChatID = u.TelegramUserID
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

func pairInviterMention(person db.User, scope db.EventScope, name string) telegram.User {
	if person.BotID != scope.BotID || person.Organization != scope.Organization || person.Revoked {
		return telegram.User{}
	}
	return telegram.User{ID: person.TelegramUserID, FirstName: name}
}

func pairNotificationContent(
	n db.PairInvitationDelivery,
	language string,
	inviter telegram.User,
) (telegram.SendMessageRequest, error) {
	text, err := pairNotificationText(n, language)
	if err != nil {
		return telegram.SendMessageRequest{}, err
	}
	input := telegram.SendMessageRequest{Text: text}
	name := fmt.Sprintf("%.200s", n.Context.InviterName)
	if n.Kind == pairNoticePending && inviter.ID > 0 && name != "" {
		if inviter.FirstName == "" {
			inviter.FirstName = name
		}
		input.Entities = []telegram.MessageEntity{{Type: "text_mention", Offset: 0,
			Length: len(utf16.Encode([]rune(name))), User: &inviter}}
	}
	return input, nil
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
	reference := fmt.Sprintf("\nEvent: %.200s\nInvitation: %s", v.EventName, n.InvitationID)
	if language == "ru" {
		reference = fmt.Sprintf(
			"\nЭвент: %.200s\nПриглашение: %s",
			v.EventName,
			n.InvitationID,
		)
	}
	if n.Kind == pairNoticePending {
		expires := v.Invitation.ExpiresAt.In(zone).Format(time.RFC3339)
		if language == "ru" {
			return fmt.Sprintf(
				"%.200s приглашает вас в пару, роль: %s.\n"+
					"Срок: %s (%s).\n"+
					"Чтобы принять, явно подтвердите приглашение боту. Принятие добавляет в очередь пары; "+
					"пасс ещё не назначен.%s",
				v.InviterName,
				v.RequiredRole,
				expires,
				v.Timezone,
				reference,
			), nil
		}
		return fmt.Sprintf(
			"%.200s invites you to pair as %s.\n"+
				"Expires: %s (%s).\n"+
				"To accept, explicitly confirm this invitation to the bot. Acceptance joins the pair waitlist; "+
				"no pass is assigned yet.%s",
			v.InviterName,
			v.RequiredRole,
			expires,
			v.Timezone,
			reference,
		), nil
	}
	texts := map[string]string{
		pairNoticeAccepted: "Your invitation was accepted. The pair joined the waitlist; " +
			"no passes were assigned by acceptance.",
		"declined": "Your invitation was declined. The invitation's allocation hold has been released.",
		"revoked":  "The inviter revoked this invitation. It can no longer be accepted.",
		"expired":  "Your invitation expired. The invitation's allocation hold has been released.",
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
