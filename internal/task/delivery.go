package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	noticeNoActiveTask  = "no_active"
	noticeStaleApproval = "stale"
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
	bot, organization, _ := s.iam.DeploymentScope()
	preference, err := s.store.ProfileLanguageByTelegram(ctx, bot, organization, i.User.ID)
	if err == nil && (preference == "en" || preference == "ru") {
		return preference
	}
	if err != nil && (i.PreferredLanguage == "en" || i.PreferredLanguage == "ru") {
		return i.PreferredLanguage
	}
	fallback := "en"
	if strings.TrimSpace(i.User.LanguageCode) == "" {
		if settings, settingsErr := s.store.Settings(ctx); settingsErr == nil {
			fallback = settings.Document.DefaultLanguage
		}
	}
	return languageChoice(i.User.LanguageCode, fallback)
}
func (s *Service) localized(ctx context.Context, i intent, key string) string {
	ru := s.languageFor(ctx, i) == "ru"
	texts := map[string][2]string{
		kindUnsupported: {
			"Пока не умею читать такие сообщения. Пришли текст, пожалуйста.",
			"I can’t read this message type yet. Please send text.",
		},
		"denied": {
			"Сейчас у тебя нет доступа. Обратись к администратору.",
			"You don’t have access right now. Please contact the administrator.",
		},
		stateFailed: {
			"Не смогла завершить задачу. Действия могли уже выполниться. Проверь их статус перед повтором.",
			"I couldn’t finish the task. Actions may already have completed. Check their status before retrying.",
		},
		stateCancelled: {
			"Отменила задачу. Уже выполненные действия могут сохраниться.",
			"I cancelled the task. Actions already completed may remain.",
		},
		noticeNoActiveTask: {"Нет активной задачи.", "There is no active task."},
		reasonCreditsExhausted: {
			"Бюджет исчерпан, поэтому я не могу продолжить. Пополни баланс или дождись следующего пополнения, " +
				"затем напиши мне ещё раз.",
			"The budget is used, so I can’t continue. Add funds or wait for the next funding, then write to me again.",
		},
		noticeStaleApproval: {
			"Этот вопрос уже не актуален. Используй моё последнее сообщение.",
			"This prompt is no longer current. Use my latest message.",
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
func (s *Service) result(ctx context.Context, i intent, text, key string) []db.Outbound {
	var out []db.Outbound
	people := s.messagePeople(ctx, i, text)
	mentions := map[int64]telegram.User{i.User.ID: i.User}
	for _, user := range people {
		mentions[user.ID] = user
	}
	for n, chunk := range render.People(text, mentions, people) {
		payload, _ := json.Marshal(
			telegram.SendMessageRequest{ChatID: i.ChatID, Text: chunk.Text, Entities: chunk.Entities},
		)
		out = append(out, db.Outbound{DedupKey: key + ":" + strconv.Itoa(n), ChunkSeq: int32(n), Payload: payload})
	}
	return out
}

var telegramReference = regexp.MustCompile(`tg://user\?id=([0-9]+)\b`)

const messagePeopleTimeout = 3 * time.Second

func messageReferences(text string) ([]uuid.UUID, []int64) {
	if len(render.AccountReferences(text)) == 0 && !telegramReference.MatchString(text) {
		return nil, nil
	}
	text = render.PersonReferenceText(text)
	matches := render.AccountReferences(text)
	links := telegramReference.FindAllStringSubmatch(text, -1)
	telegramIDs := make([]int64, 0, len(links))
	seenTelegram := make(map[int64]bool)
	for _, link := range links {
		if id, err := strconv.ParseInt(link[1], 10, 64); err == nil && id > 0 && !seenTelegram[id] {
			telegramIDs = append(telegramIDs, id)
			seenTelegram[id] = true
		}
	}
	ids := make([]uuid.UUID, 0, len(matches))
	seen := make(map[uuid.UUID]bool)
	for _, match := range matches {
		id, err := uuid.Parse(match)
		if err == nil && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, telegramIDs
}

func (s *Service) messagePeople(ctx context.Context, i intent, text string) map[string]telegram.User {
	ids, telegramIDs := messageReferences(text)
	if len(ids)+len(telegramIDs) == 0 {
		return nil
	}
	bot, organization, _ := s.iam.DeploymentScope()
	lookup, cancel := context.WithTimeout(ctx, messagePeopleTimeout)
	defer cancel()
	actor, err := s.store.MessageRecipient(lookup, bot, organization, i.User.ID)
	if err != nil {
		return nil
	}
	scope := db.PaymentScope{
		BotID:        bot,
		Organization: organization,
		UserID:       actor.ID,
		Subject:      actor.Subject,
	}
	people, err := s.store.MessagePeople(lookup, scope, ids, telegramIDs)
	if err != nil {
		s.logger.WarnContext(ctx, "message people lookup failed", "class", "message_people")
		return nil
	}
	return people
}

func (s *Service) question(ctx context.Context, i intent, q loom.Question) []db.Outbound {
	out := s.result(ctx, i, q.Text, "question:dialog-v1:"+keyboardPromptKey(q))
	if len(q.Choices) > 0 {
		setReplyMarkup(out, questionKeyboard(q))
	} else {
		removeReplyKeyboard(out)
	}
	recordPromptContext(out, loom.InputPresentation{QuestionID: q.ID})
	return out
}
func (s *Service) approval(ctx context.Context, i intent, id uuid.UUID, a loom.Approval) []db.Outbound {
	text := a.Text
	if approvalHasArguments(a) && !completeServiceApproval(a) {
		if !json.Valid(a.Arguments) {
			s.logger.WarnContext(ctx, "approval arguments invalid", "class", "approval_arguments")
			return nil
		}
		text += "\n\n" + approvalParameters(a.Arguments, s.languageFor(ctx, i))
	}
	return s.approvalContent(ctx, i, id, a, text)
}

// These preparations provide all human terms in the service description.
func completeServiceApproval(a loom.Approval) bool {
	return strings.TrimSpace(a.Text) != "" &&
		(a.Method == "zinusya_massage.book" || a.Method == "zinusya_massage.cancel" ||
			a.Method == "zinusya_massage_staff.instant" ||
			a.Method == "zinusya_administration.set_massage_specialist_binding")
}

// Presentation can change without changing the pending action's identity.
func (s *Service) approvalContent(
	ctx context.Context,
	i intent,
	id uuid.UUID,
	a loom.Approval,
	text string,
) []db.Outbound {
	if s.options.DebugApprovalJSON && approvalHasArguments(a) {
		var arguments bytes.Buffer
		if err := json.Indent(&arguments, a.Arguments, "", "  "); err != nil {
			s.logger.WarnContext(ctx, "approval debug arguments invalid", "class", "approval_arguments")
		} else {
			fence := approvalFence(arguments.String())
			text += "\n\n" + fence + "json\n" + arguments.String() + "\n" + fence
		}
	}
	const attention = "⚠️"
	title, yes, no := "Approval required", "✅ Approve", "❌ Decline"
	if s.languageFor(ctx, i) == "ru" {
		title, yes, no = "Необходимо подтверждение", "✅ Подтвердить", "❌ Отклонить"
	}
	key := "approval:dialog-v1:" + keyboardPromptKey(a)
	out := s.result(ctx, i, attention+" **"+title+"** "+attention+"\n\n"+text, key)
	if len(out) == 0 {
		return nil
	}
	var payload telegram.SendMessageRequest
	_ = json.Unmarshal(out[len(out)-1].Payload, &payload)
	// Only offer a decision when the approval body is visible after rendering.
	if len(out) == 1 && strings.TrimSpace(payload.Text) == attention+" "+title+" "+attention {
		return nil
	}
	switch a.ReplyMode {
	case loom.ReplyModeAgent:
		payload.ReplyMarkup = approvalKeyboard(yes, no)
	case loom.ReplyModeProtocol:
		prompt := approvalPromptKey(a)
		payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{
			{Text: yes, CallbackData: approvalCallbackData(id, prompt, approvalApprove)},
			{Text: no, CallbackData: approvalCallbackData(id, prompt, approvalReject)},
		}}}
	default:
		return nil
	}
	out[len(out)-1].Payload, _ = json.Marshal(payload)
	recordPromptContext(out, loom.InputPresentation{ApprovalID: a.ID})
	return out
}

func approvalHasArguments(a loom.Approval) bool {
	arguments := bytes.TrimSpace(a.Arguments)
	return len(arguments) > 0 && !bytes.Equal(arguments, []byte("null"))
}
func (s *Service) finish(ctx context.Context, t *db.Task, c checkpoint, i intent, state, key string) error {
	t.State = state
	out := s.result(ctx, i, s.localized(ctx, i, key), key)
	if c.KeyboardPrompt != "" {
		removeReplyKeyboard(out)
		c.KeyboardPrompt = ""
	}
	if i.Kind == kindApproval && (key == noticeStaleApproval || key == noticeNoActiveTask) {
		var err error
		out, err = s.closeRunApprovalKeyboard(ctx, *t, i, out)
		if err != nil {
			return s.wait()
		}
	} else {
		out = editCallbackOutput(i, out)
	}
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
		case deliverySending:
			// A new fenced task attempt owns recovery; a former sender can only win this CAS first.
			receipt, finishErr := s.store.FinishDelivery(ctx, row.ID, row.AttemptID, stateDeliveryUnknown, nil)
			err = finishErr
			if err != nil {
				s.reportDatabaseFailure(ctx, t.ID, "delivery_reconcile", err)
				return s.wait()
			}
			row = receipt.Delivery
			return s.gateDelivery(ctx, t, c, row)
		case stateDeliveryUnknown, stateDeliveryFailed:
			return s.gateDelivery(ctx, t, c, row)
		case "delivery_blocked":
			blocked = true
			continue
		case deliveryPending, deliveryRetryable:
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
		!strings.HasPrefix(row.DedupKey, prefix+"approval:") &&
		!strings.HasPrefix(row.DedupKey, prefix+stepLimitPromptPrefix) &&
		!strings.HasPrefix(row.DedupKey, prefix+"avatar:") {
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
	unlock, sendErr := s.lockKeyboardDelivery(ctx, row)
	defer unlock()
	var receipt telegram.Receipt
	var keyboardHash string
	if sendErr == nil {
		receipt, keyboardHash, sendErr = s.sendOutbound(ctx, row)
	}
	sentAt := ""
	if keyboardHash != "" {
		sentAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	outcome, delay := deliveryOutcome(sendErr)
	data, _ := json.Marshal(struct {
		MessageID      int64  `json:"message_id"`
		ChatID         int64  `json:"chat_id"`
		KeyboardHash   string `json:"reply_keyboard_hash,omitempty"`
		KeyboardSentAt string `json:"reply_keyboard_sent_at,omitempty"`
	}{receipt.MessageID, receipt.ChatID, keyboardHash, sentAt})
	// Preserve the outcome through cancellation, with a bounded inherited context.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer finishCancel()
	finished, err := s.store.FinishDelivery(finishCtx, row.ID, row.AttemptID, outcome, data)
	if err != nil {
		s.reportDatabaseFailure(finishCtx, row.TaskID, "delivery_receipt", err)
	}
	if err == nil && finished.ResponseElapsedSeconds != nil && s.observer != nil {
		s.observer.RecordResponseLatency(finishCtx, *finished.ResponseElapsedSeconds)
	}
	metricOutcome := metricSuccess
	switch {
	case err != nil:
		metricOutcome = metricError
	case outcome == deliveryRetryable:
		metricOutcome = metricRetry
	case outcome != deliveryDelivered:
		metricOutcome = metricError
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
