package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/complynx/shuttle/internal/db"
	"github.com/complynx/shuttle/internal/identity"
	"github.com/complynx/shuttle/internal/loom"
	"github.com/complynx/shuttle/internal/render"
	"github.com/complynx/shuttle/internal/telegram"
)

func languageChoice(code, fallback string) string {
	if strings.TrimSpace(code) == "" {
		code = fallback
	}
	base := strings.ToLower(strings.Split(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"), "-")[0])
	switch base {
	case "ru", "be", "uk":
		return "ru"
	default:
		return "en"
	}
}
func (s *Service) languageFor(ctx context.Context, i intent) string {
	fallback := "en"
	if strings.TrimSpace(i.User.LanguageCode) == "" {
		if settings, err := s.store.Settings(ctx); err == nil {
			fallback = settings.Document.DefaultLanguage
		}
	}
	return languageChoice(i.User.LanguageCode, fallback)
}
func (s *Service) localized(ctx context.Context, i intent, key string) string {
	ru := s.languageFor(ctx, i) == "ru"
	texts := map[string][2]string{
		kindUnsupported: {
			"Этот тип сообщения пока не поддерживается. Пожалуйста, отправьте текст.",
			"This message type is not supported yet. Please send text.",
		},
		"denied": {
			"Доступ запрещён. Обратитесь к администратору.",
			"Access denied. Please contact the administrator.",
		},
		stateFailed: {"Не удалось выполнить задачу.", "The task could not be completed."},
		stateCancelled: {
			"Задача отменена. Уже выполненные действия могут сохраниться.",
			"Task cancelled. Actions already completed may remain.",
		},
		"no_active": {"Нет активной задачи.", "There is no active task."},
		"stale": {
			"Этот вопрос уже не актуален. Используйте последнее сообщение бота.",
			"This prompt is no longer current. Use the bot's latest message.",
		},
		stateAuthUnknown: {
			"Результат обновления авторизации неизвестен. Задача приостановлена для проверки администратором.",
			"The authorization outcome is unknown. The task is paused for administrator review.",
		},
	}
	value := texts[key]
	if ru {
		return value[0]
	}
	return value[1]
}
func (s *Service) result(i intent, text, key string) []db.Outbound {
	var out []db.Outbound
	for n, chunk := range render.Render(text, map[int64]telegram.User{i.User.ID: i.User}) {
		payload, _ := json.Marshal(
			telegram.SendMessageRequest{ChatID: i.ChatID, Text: chunk.Text, Entities: chunk.Entities},
		)
		out = append(out, db.Outbound{DedupKey: key + ":" + strconv.Itoa(n), ChunkSeq: int32(n), Payload: payload})
	}
	return out
}
func (s *Service) question(i intent, id uuid.UUID, q loom.Question) []db.Outbound {
	key := "question:" + promptKey(q)
	out := s.result(i, q.Text, key)
	if len(out) == 0 {
		return nil
	}
	var markup telegram.InlineKeyboardMarkup
	for n, choice := range q.Choices {
		if n >= maxChoices {
			break
		}
		markup.InlineKeyboard = append(
			markup.InlineKeyboard,
			[]telegram.InlineKeyboardButton{
				{Text: choice.Label, CallbackData: fmt.Sprintf("q:%s:%s:%d", id, promptKey(q), n)},
			},
		)
	}
	if len(markup.InlineKeyboard) > 0 {
		var payload telegram.SendMessageRequest
		_ = json.Unmarshal(out[len(out)-1].Payload, &payload)
		payload.ReplyMarkup = &markup
		out[len(out)-1].Payload, _ = json.Marshal(payload)
	}
	return out
}
func (s *Service) approval(ctx context.Context, i intent, id uuid.UUID, a loom.Approval) []db.Outbound {
	key := "approval:" + promptKey(a)
	out := s.result(i, a.Text, key)
	if len(out) == 0 {
		return nil
	}
	var payload telegram.SendMessageRequest
	_ = json.Unmarshal(out[len(out)-1].Payload, &payload)
	yes, no := "Approve", "Reject"
	if s.languageFor(ctx, i) == "ru" {
		yes, no = "Подтвердить", "Отклонить"
	}
	payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: yes, CallbackData: fmt.Sprintf("a:%s:%s:approve", id, promptKey(a))},
				{Text: no, CallbackData: fmt.Sprintf("a:%s:%s:reject", id, promptKey(a))},
			},
		},
	}
	out[len(out)-1].Payload, _ = json.Marshal(payload)
	return out
}
func (s *Service) finish(ctx context.Context, t *db.Task, c checkpoint, i intent, state, key string) error {
	t.State = state
	out := s.result(i, s.localized(ctx, i, key), key)
	if e := s.save(ctx, t, c, out); e != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}
func (s *Service) deliver(ctx context.Context, t *db.Task, c checkpoint) error {
	rows, err := s.store.ListOutbox(ctx, t.ID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_read", err)
		return s.wait()
	}
	blocked := false
	for _, row := range rows {
		switch row.State {
		case deliveryDelivered:
			continue
		case "send_inflight":
			// A new fenced task attempt owns recovery; a former sender can only win this CAS first.
			row, err = s.store.FinishDelivery(ctx, row.ID, row.AttemptID, stateDeliveryUnknown, nil)
			if err != nil {
				s.reportDatabaseFailure(ctx, t.ID, "delivery_reconcile", err)
				return s.wait()
			}
			return s.gateDelivery(ctx, t, c, row)
		case stateDeliveryUnknown, stateDeliveryFailed:
			return s.gateDelivery(ctx, t, c, row)
		case "delivery_blocked":
			blocked = true
			continue
		case "pending", deliveryRetryable:
		default:
			return s.wait()
		}
		allowed, accessErr := s.deliveryAllowed(ctx, t, row)
		if accessErr != nil {
			return s.wait()
		}
		if !allowed {
			blocked = true
			continue
		}
		more, sendErr := s.deliverPending(ctx, t, c, row)
		if !more {
			return sendErr
		}
	}
	if blocked && t.State == stateCompleted {
		return s.recordDeliveryState(ctx, t, c, stateDeliveryFailed)
	}
	return nil
}

func (s *Service) deliverPending(ctx context.Context, t *db.Task, c checkpoint, row db.Delivery) (bool, error) {
	reserved, err := s.store.ReserveDelivery(ctx, row.ID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_reserve", err)
		return false, s.wait()
	}
	outcome, delay, err := s.sendDelivery(ctx, reserved)
	if err != nil {
		return false, s.wait()
	}
	if outcome == deliveryRetryable {
		if delay <= 0 {
			delay = s.options.PollInterval
		}
		return false, river.JobSnooze(delay)
	}
	if outcome == stateDeliveryUnknown || outcome == stateDeliveryFailed {
		reserved.State = outcome
		return false, s.gateDelivery(ctx, t, c, reserved)
	}
	return true, nil
}

func (s *Service) deliveryAllowed(ctx context.Context, t *db.Task, row db.Delivery) (bool, error) {
	// Access-denied notifications contain no run output and remain deliverable.
	if strings.HasPrefix(row.DedupKey, t.ID.String()+":denied:") {
		return true, nil
	}
	u, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_user_read", err)
		return false, err
	}
	if u.Revoked {
		return s.blockUnauthorizedDelivery(ctx, t, row, u.Subject)
	}
	err = s.verifyDeliveryAuthorization(ctx, u, row)
	if errors.Is(err, identity.ErrDenied) || errors.Is(err, identity.ErrConflict) {
		return s.blockUnauthorizedDelivery(ctx, t, row, u.Subject)
	}
	if err != nil {
		return false, err
	}
	u, err = s.store.GetUser(ctx, t.UserID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_user_recheck", err)
		return false, err
	}
	if u.Revoked {
		return s.blockUnauthorizedDelivery(ctx, t, row, u.Subject)
	}
	return true, nil
}

func (s *Service) blockUnauthorizedDelivery(
	ctx context.Context, t *db.Task, row db.Delivery, subject string,
) (bool, error) {
	s.iam.Invalidate(subject)
	err := s.store.BlockPendingDelivery(ctx, row.ID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_revoke", err)
	}
	return false, err
}

func (s *Service) verifyDeliveryAuthorization(ctx context.Context, u db.User, row db.Delivery) error {
	prefix := row.TaskID.String() + ":"
	if !strings.HasPrefix(row.DedupKey, prefix+"result:") &&
		!strings.HasPrefix(row.DedupKey, prefix+"question:") &&
		!strings.HasPrefix(row.DedupKey, prefix+"approval:") {
		return nil
	}
	credential, err := s.iam.UserToken(ctx, u.Subject)
	if err != nil {
		return err
	}
	_, err = s.iam.Verify(ctx, credential.Token)
	return err
}
func (s *Service) gateDelivery(ctx context.Context, t *db.Task, c checkpoint, row db.Delivery) error {
	if err := s.store.BlockDeliverySuffix(ctx, t.ID, row.ChunkSeq); err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "delivery_block", err)
		return s.wait()
	}
	return s.recordDeliveryState(ctx, t, c, row.State)
}
func (s *Service) recordDeliveryState(ctx context.Context, t *db.Task, c checkpoint, state string) error {
	if state != stateDeliveryUnknown && state != stateDeliveryFailed {
		return nil
	}
	if state == stateDeliveryUnknown {
		s.logger.WarnContext(ctx, "outbound delivery needs reconciliation", "class", stateDeliveryUnknown)
	}
	// Keep active run execution and cancellation independent of delivery uncertainty.
	if terminal(t.State) && t.State != state {
		t.State = state
		if err := s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
	}
	return nil
}
func (s *Service) sendDelivery(ctx context.Context, row db.Delivery) (string, time.Duration, error) {
	var payload telegram.SendMessageRequest
	if json.Unmarshal(row.Payload, &payload) != nil {
		return "", 0, errors.New("invalid outbound payload")
	}
	receipt, sendErr := s.tg.SendMessage(ctx, payload)
	var rejected *telegram.Error
	if errors.As(sendErr, &rejected) && rejected.InvalidEntities && rejected.Outcome == telegram.Rejected &&
		len(payload.Entities) > 0 {
		payload.Entities = nil
		receipt, sendErr = s.tg.SendMessage(ctx, payload)
	}
	outcome, delay := deliveryOutcome(sendErr)
	data, _ := json.Marshal(map[string]int64{"message_id": receipt.MessageID, "chat_id": receipt.ChatID})
	// Preserve the outcome through cancellation, with a bounded inherited context.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer finishCancel()
	_, err := s.store.FinishDelivery(finishCtx, row.ID, row.AttemptID, outcome, data)
	if err != nil {
		s.reportDatabaseFailure(finishCtx, row.TaskID, "delivery_receipt", err)
	}
	metricOutcome := "success"
	switch {
	case err != nil:
		metricOutcome = "error"
	case outcome == deliveryRetryable:
		metricOutcome = "retry"
	case outcome != deliveryDelivered:
		metricOutcome = "error"
	}
	s.record(ctx, "deliver", metricOutcome)
	return outcome, delay, err
}
func deliveryOutcome(err error) (string, time.Duration) {
	if err == nil {
		return deliveryDelivered, 0
	}
	var upstream *telegram.Error
	if !errors.As(err, &upstream) || upstream.Outcome == telegram.Unknown {
		return stateDeliveryUnknown, 0
	}
	if upstream.Retryable || upstream.Outcome == telegram.NotStarted {
		return deliveryRetryable, upstream.RetryAfter
	}
	return stateDeliveryFailed, 0
}
