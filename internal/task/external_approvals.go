package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const externalApprovalPrefix = "ma:"
const externalApprovalAll = "all"
const externalApprovalRejected = "rejected"
const externalApprovalSendTimeout = 45 * time.Second
const externalApprovalService = "zinusya"
const externalApprovalPrefixUnits = 104 // UUID, part counters and presentation reference.

type externalApprovalAttempt struct {
	operation, lease uuid.UUID
	presentation     [sha256.Size]byte
	nextPart         int
}

type externalApprovalChoice struct {
	ID     uuid.UUID `json:"id"`
	Choice string    `json:"choice"`
}

func externalApprovalCallback(i intent, cb *telegram.CallbackQuery) (intent, bool, error) {
	value, matched := strings.CutPrefix(cb.Data, externalApprovalPrefix)
	if !matched {
		return i, false, nil
	}
	raw, choice, valid := strings.Cut(value, ":")
	id, err := uuid.Parse(raw)
	if !valid || err != nil || id == uuid.Nil || id.String() != raw || cb.Message.ID <= 0 {
		return i, true, ErrUnsupportedUpdate
	}
	switch choice {
	case "once", "method", externalApprovalAll, "reject":
		i.Kind, i.StructuredCommand, i.Text = kindStructured, externalAgentCommand, externalAgentCommand
		i.ExternalApproval = &externalApprovalChoice{ID: id, Choice: choice}
		i.CallbackMessageID = cb.Message.ID
		return i, true, nil
	default:
		return i, true, ErrUnsupportedUpdate
	}
}

// DeliverExternalApproval presents a Loom decision directly in the owner's
// private Telegram chat. The operation and message IDs bind each callback.
func (s *Service) DeliverExternalApproval(ctx context.Context, approval loom.ExternalApproval) error {
	if !s.options.ExternalAgentWrites || approval.OrgID == "" || !approval.ExpiresAt.After(time.Now()) {
		return identity.ErrDenied
	}
	bot, org, _ := s.iam.DeploymentScope()
	operation, err := uuid.Parse(approval.ID)
	if err != nil || operation == uuid.Nil || approval.OrgID != org || approval.Subject == "" {
		return identity.ErrDenied
	}
	user, err := s.store.GetUserBySubject(ctx, bot, org, approval.Subject)
	if err != nil || user.Revoked || user.TelegramUserID <= 0 {
		return identity.ErrDenied
	}
	if err = s.telegramUserAvailable(ctx, user); err != nil {
		return err
	}
	return s.sendExternalApproval(ctx, user, operation, approval)
}

func (s *Service) sendExternalApproval(
	ctx context.Context, user db.User, id uuid.UUID, approval loom.ExternalApproval,
) error {
	messages, keyboard, err := s.externalApprovalPresentation(ctx, user, approval)
	if err != nil {
		return err
	}
	rendered, err := json.Marshal(struct {
		Messages []render.Message               `json:"messages"`
		Keyboard *telegram.InlineKeyboardMarkup `json:"keyboard"`
	}{messages, keyboard})
	if err != nil {
		return err
	}
	attempt := externalApprovalAttempt{operation: id, lease: uuid.New(), presentation: sha256.Sum256(rendered)}
	nextPart, claimed, err := s.store.ClaimExternalApproval(
		ctx, id, user.ID, attempt.lease, approval.ExpiresAt, attempt.presentation[:],
	)
	if err != nil {
		return err
	}
	if !claimed {
		delivered, readErr := s.store.ExternalApprovalDelivered(ctx, id, user.ID)
		if readErr != nil {
			return readErr
		}
		if delivered {
			return nil
		}
		return identity.ErrUnavailable
	}
	if nextPart >= len(messages) {
		return identity.ErrUnavailable
	}
	attempt.nextPart = nextPart
	// Respect the caller's deadline and cap sending below the delivery lease.
	ctx, cancel := context.WithTimeout(ctx, min(time.Until(approval.ExpiresAt), externalApprovalSendTimeout))
	defer cancel()
	return s.sendExternalApprovalMessages(ctx, user.TelegramUserID, attempt, messages, keyboard)
}

func (s *Service) sendExternalApprovalMessages(
	ctx context.Context, chatID int64, attempt externalApprovalAttempt, messages []render.Message,
	keyboard *telegram.InlineKeyboardMarkup,
) error {
	for index := attempt.nextPart; index < len(messages); index++ {
		message := messages[index]
		const presentationLabelBytes = 4
		header := fmt.Sprintf("MCP %s [%d/%d] %x\n",
			attempt.operation, index+1, len(messages), attempt.presentation[:presentationLabelBytes])
		for n := range message.Entities {
			message.Entities[n].Offset += len(header)
		}
		request := telegram.SendMessageRequest{
			ChatID: chatID, Text: header + message.Text, Entities: message.Entities,
			LinkPreviewOptions: &telegram.LinkPreviewOptions{IsDisabled: true},
		}
		last := index == len(messages)-1
		if last {
			request.ReplyMarkup = keyboard
		}
		if err := s.sendExternalApprovalPart(ctx, attempt, index, request); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sendExternalApprovalPart(
	ctx context.Context, attempt externalApprovalAttempt, index int, request telegram.SendMessageRequest,
) error {
	receipt, sendErr := s.tg.SendMessage(ctx, request)
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	last := request.ReplyMarkup != nil
	if sendErr != nil {
		failure, known := errors.AsType[*telegram.Error](sendErr)
		// An uncertain keyboard send keeps its lease. A retry resumes at this part.
		if last && (!known || failure.Outcome == telegram.Unknown) {
			return sendErr
		}
		var delay time.Duration
		if known {
			delay = failure.RetryAfter
			if failure.StatusCode == http.StatusTooManyRequests {
				delay = max(delay, time.Second)
			}
		}
		_ = s.store.ReleaseExternalApproval(finish, attempt.operation, attempt.lease, delay)
		return sendErr
	}
	var keyboardMessage int64
	if last {
		keyboardMessage = receipt.MessageID
	}
	return s.store.SaveExternalApprovalPart(finish, attempt.operation, attempt.lease, index, keyboardMessage)
}

func (s *Service) externalApprovalPresentation(
	ctx context.Context, user db.User, approval loom.ExternalApproval,
) ([]render.Message, *telegram.InlineKeyboardMarkup, error) {
	var profile telegram.User
	_ = json.Unmarshal(user.Profile, &profile)
	profile.ID = user.TelegramUserID
	i := intent{User: profile, ChatID: profile.ID}
	language := s.languageFor(ctx, i)
	if approval.Service != externalApprovalService {
		text, keyboard := externalApprovalMessage(approval, language == "ru")
		return render.PlainWithReserve(text, externalApprovalPrefixUnits), keyboard, nil
	}
	prepared := loom.Approval{
		OperationID: approval.ID, Method: approval.Module + "." + approval.Method,
		Arguments: approval.Arguments, ExpiresAt: approval.ExpiresAt, Text: approval.Description,
	}
	if !approvalHasArguments(prepared) || !json.Valid(prepared.Arguments) {
		return nil, nil, identity.ErrUnavailable
	}
	detail, _, err := s.detailedApprovalText(ctx, user.ID, i, prepared)
	if err != nil {
		return nil, nil, err
	}
	if detail != "" {
		approval.Description = detail
	}
	text, keyboard := externalApprovalMessage(approval, language == "ru")
	if detail != "" || completeServiceApproval(prepared) {
		return render.PlainWithReserve(text, externalApprovalPrefixUnits), keyboard, nil
	}
	parameters := approvalParameters(prepared.Arguments, language)
	markdown := escapeMarkdownText(text) + "\n\n" + parameters
	return render.MarkdownWithReserve(markdown, externalApprovalPrefixUnits), keyboard, nil
}

func externalApprovalMessage(approval loom.ExternalApproval, ru bool) (string, *telegram.InlineKeyboardMarkup) {
	label := approval.Service + " / " + approval.Module + "." + approval.Method
	title := structuredText(ru, "External MCP request", "Запрос внешнего MCP-агента")
	text := fmt.Sprintf("%s · %s\n%s\n\n%s", title, approval.Client, label, approval.Description)
	button := func(label, choice string) []telegram.InlineKeyboardButton {
		return []telegram.InlineKeyboardButton{{
			Text: label, CallbackData: externalApprovalPrefix + approval.ID + ":" + choice,
		}}
	}
	rows := [][]telegram.InlineKeyboardButton{
		button(structuredText(ru, "Approve once", "Подтвердить один раз"), "once"),
		button(structuredText(ru, "Reject", "Отклонить"), "reject"),
	}
	if approval.Remember {
		text += structuredText(ru,
			"\n\nRemembered choices apply until this connection ends. Eligible writes may change data without another prompt.",
			"\n\nВыбор сохраняется до конца подключения. Допустимые записи могут менять данные без нового запроса.")
		rows = append(rows,
			button(structuredText(ru,
				"Allow this method for this connection", "Разрешить этот метод до конца подключения"), "method"),
			button(structuredText(ru, "Allow all eligible writes for this connection",
				"Разрешить все допустимые записи до конца подключения"), externalApprovalAll))
	}
	return text, &telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

type externalApprovalEditor interface {
	EditMessageText(context.Context, telegram.EditMessageRequest) (telegram.Receipt, error)
}

func (s *Service) decideExternalApproval(ctx context.Context, t *db.Task, checkpoint checkpoint, i intent) error {
	ru := s.languageFor(ctx, i) == "ru"
	inactive := structuredText(ru, "This MCP approval is no longer pending.", "Этот запрос MCP уже закрыт или истёк.")
	if !s.options.ExternalAgentWrites {
		return s.finishStructuredKeyboard(ctx, t, checkpoint, i, inactive, nil, ru)
	}
	user, service, err := s.authorize(ctx, *t, i)
	if err != nil {
		return s.identityError(ctx, t, checkpoint, i, err)
	}
	allowed, err := s.store.CanAnswerExternalApproval(ctx, i.ExternalApproval.ID, user.ID, i.CallbackMessageID)
	if err != nil {
		return s.wait()
	}
	if !allowed {
		stale := structuredText(ru, "This message cannot confirm the request. Use the latest approval message.",
			"Это сообщение не может подтвердить запрос. Используй последнее сообщение с кнопками.")
		return s.finishStructuredKeyboard(ctx, t, checkpoint, i, stale, nil, ru)
	}
	_, subject, err := s.taskIdentity(ctx, user)
	if err != nil {
		return s.identityError(ctx, t, checkpoint, i, err)
	}
	auth := loom.Auth{ServiceToken: service.Token, SubjectToken: subject.Token}
	status, err := s.loom.DecideExternalApproval(ctx, auth, i.ExternalApproval.ID.String(), i.ExternalApproval.Choice)
	if err != nil {
		upstream, ok := errors.AsType[*loom.Error](err)
		if !ok || upstream.StatusCode != http.StatusNotFound && upstream.StatusCode != http.StatusConflict &&
			upstream.StatusCode != http.StatusForbidden {
			return s.wait()
		}
	}
	result := inactive
	switch status {
	case "approved":
		result = structuredText(ru, "Approved. The external agent can continue this action.",
			"Подтверждено. Внешний агент может продолжить действие.")
		switch i.ExternalApproval.Choice {
		case "method":
			result = structuredText(ru, "Approved. This method is allowed until the MCP connection ends.",
				"Подтверждено. Метод разрешён до конца подключения MCP.")
		case externalApprovalAll:
			result = structuredText(ru, "Approved. Eligible writes are allowed until the MCP connection ends.",
				"Подтверждено. Допустимые записи разрешены до конца подключения MCP.")
		}
	case externalApprovalRejected:
		result = structuredText(ru, "MCP request rejected.", "Запрос MCP отклонён.")
	}
	if editor, ok := s.tg.(externalApprovalEditor); ok {
		_, _ = editor.EditMessageText(ctx, telegram.EditMessageRequest{
			ChatID: i.ChatID, MessageID: i.CallbackMessageID, Text: "MCP " + i.ExternalApproval.ID.String() + "\n\n" + result,
			ReplyMarkup: &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{}},
		})
	}
	return s.finishStructuredKeyboard(ctx, t, checkpoint, i, result, nil, ru)
}
