package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/render"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const externalConnectionPrefix = "mc:"

func externalConnectionCallback(i intent, data string) (intent, bool, error) {
	value, matched := strings.CutPrefix(data, externalConnectionPrefix)
	if !matched {
		return i, false, nil
	}
	raw, choice, found := strings.Cut(value, ":")
	id, err := uuid.Parse(raw)
	if !found || err != nil || id == uuid.Nil || id.String() != raw || i.CallbackMessageID <= 0 ||
		(choice != approvalApprove && choice != approvalReject) {
		return i, true, ErrUnsupportedUpdate
	}
	i.Kind, i.StructuredCommand, i.Text = kindStructured, externalAgentCommand, externalAgentCommand
	i.ExternalConnection = &externalApprovalChoice{ID: id, Choice: choice}
	return i, true, nil
}

// DeliverExternalConnectionApproval presents a new OAuth connection before Loom releases a code.
func (s *Service) DeliverExternalConnectionApproval(
	ctx context.Context,
	approval loom.ExternalConnectionApproval,
) error {
	if !s.options.ExternalAgentEnabled || approval.Subject == "" || approval.Client == "" ||
		len(approval.Client) > 96 || len(approval.Redirect) > 1024 ||
		strings.ContainsFunc(approval.Client+approval.Redirect, func(r rune) bool { return !unicode.IsPrint(r) }) ||
		!approval.ExpiresAt.After(time.Now()) {
		return identity.ErrDenied
	}
	bot, org, issuer := s.iam.DeploymentScope()
	id, err := uuid.Parse(approval.ID)
	if err != nil || id == uuid.Nil || approval.OrgID != org || approval.Issuer != issuer {
		return identity.ErrDenied
	}
	user, err := s.store.GetUserBySubject(ctx, bot, org, approval.Subject)
	if err != nil || user.Revoked || user.TelegramUserID <= 0 {
		return identity.ErrDenied
	}
	if err = s.telegramUserAvailable(ctx, user); err != nil {
		return err
	}
	var profile telegram.User
	_ = json.Unmarshal(user.Profile, &profile)
	profile.ID = user.TelegramUserID
	ru := s.languageFor(ctx, intent{User: profile, ChatID: profile.ID}) == "ru"
	text := fmt.Sprintf(structuredText(ru,
		"Authorize MCP client %q?\nCallback: %s\n\nApprove if you started this connection.",
		"Авторизовать MCP-клиент %q?\nАдрес возврата: %s\n\nПодтверди, если ты начал это подключение."),
		approval.Client, approval.Redirect)
	keyboard := &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{
		{Text: structuredText(ru, "Approve", "Разрешить"), CallbackData: externalConnectionPrefix + id.String() + ":approve"},
		{Text: structuredText(ru, "Reject", "Отклонить"), CallbackData: externalConnectionPrefix + id.String() + ":reject"},
	}}}
	return s.sendExternalPrompt(
		ctx,
		user,
		id,
		approval.ExpiresAt,
		render.PlainWithReserve(text, externalApprovalPrefixUnits),
		keyboard,
	)
}

func (s *Service) decideExternalConnection(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	ru := s.languageFor(ctx, i) == "ru"
	stale := structuredText(ru, "This MCP connection request has expired.", "Запрос на подключение MCP истёк.")
	user, service, err := s.authorize(ctx, *t, i)
	if err != nil {
		i.CallbackMessageID = 0
		return s.identityError(ctx, t, c, i, err)
	}
	decision := i.ExternalConnection
	matched, expired, err := s.store.ExternalApprovalCallback(ctx, decision.ID, user.ID, i.CallbackMessageID)
	if err != nil {
		return s.wait()
	}
	if !matched {
		i.CallbackMessageID = 0
	}
	if !matched || expired || !s.options.ExternalAgentEnabled {
		return s.finishStructuredKeyboard(ctx, t, c, i, stale, nil, ru)
	}
	_, subject, err := s.taskIdentity(ctx, user)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service.Token, SubjectToken: subject.Token}
	err = s.loom.DecideExternalConnection(ctx, auth, decision.ID.String(), decision.Choice)
	if err != nil {
		upstream, known := errors.AsType[*loom.Error](err)
		if !known || (upstream.StatusCode != http.StatusForbidden && upstream.StatusCode != http.StatusConflict &&
			upstream.StatusCode != http.StatusNotFound) {
			return s.wait()
		}
		return s.finishStructuredKeyboard(ctx, t, c, i, stale, nil, ru)
	}
	text := structuredText(
		ru,
		"MCP connection approved. Return to the client.",
		"Подключение MCP разрешено. Вернись в клиент.",
	)
	if decision.Choice == approvalReject {
		text = structuredText(ru, "MCP connection rejected.", "Подключение MCP отклонено.")
	}
	return s.finishStructuredKeyboard(ctx, t, c, i, text, nil, ru)
}
