package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const adminMessagePrefix = "adminmsg:"

func isAdminMessageUpdate(in incoming, u telegram.Update) bool {
	return (u.Callback != nil && strings.HasPrefix(in.text, adminMessagePrefix)) ||
		(u.Message != nil && (in.text == "/send_message_to" || strings.HasPrefix(in.text, "/send_message_to ")))
}

func (b *Bot) handleAdminMessage(ctx context.Context, in incoming, u telegram.Update) error {
	ctx = withAdminMessageSource(ctx, in, u)
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	messages := &orderMessages{language: prefs.Language}
	if u.Callback != nil {
		defer b.acknowledge(ctx, u.Callback.ID)
	}
	err = b.authorizeAdminMessage(ctx, in.owner)
	if err == nil {
		if u.Callback != nil {
			err = b.handleAdminMessageCallbackView(ctx, in, messages)
		} else {
			err = b.handleAdminMessageCommand(ctx, in, u, messages)
		}
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < 500 {
		return b.queueBotResult(
			ctx,
			in.owner,
			in.chat,
			u.ID,
			"admin_failure",
			botdelivery.Reference{Family: botFamilyStatic},
			botdelivery.StoredResult{
				Notice: i18n.AdminMessageFailed,
				Values: map[string]string{orderCodeParameter: problem.Code},
			}, 0,
		)
	}
	return err
}

func (b *Bot) authorizeAdminMessage(ctx context.Context, owner string) error {
	var access struct {
		Allowed bool `json:"allowed"`
	}
	if err := b.API.Call(ctx, owner, http.MethodPost, "/v1/admin-messages/capabilities", nil, &access); err != nil {
		return err
	}
	if !access.Allowed {
		return &core.ProblemError{Status: http.StatusForbidden, Code: mediaForbidden}
	}
	return nil
}

func (b *Bot) handleAdminMessageCommand(
	ctx context.Context,
	in incoming,
	u telegram.Update,
	messages *orderMessages,
) error {
	if in.text == "/send_message_to" {
		return b.sendAdminMessageView(ctx, in.chat, messages.text(i18n.AdminMessageHelp, nil), 0, messages)
	}
	raw, err := telegram.CommandText(*u.Message)
	if err != nil {
		return err
	}
	command, err := adminmessage.ParseCommand(raw)
	if err != nil {
		return err
	}
	if command.NeedsInput() {
		return b.beginAdminMessageInput(ctx, in, raw, fmt.Sprintf("tg-admin-%d", u.ID), messages)
	}
	var preview adminmessage.Message
	err = b.API.Call(
		ctx,
		in.owner,
		http.MethodPost,
		"/v1/admin-messages/preview",
		map[string]string{"key": fmt.Sprintf("tg-admin-%d", u.ID), "command": raw},
		&preview,
	)
	if err != nil {
		return err
	}
	return b.sendAdminMessagePage(ctx, in, preview.ID, 0, messages)
}

func (b *Bot) handleAdminMessageCallbackView(ctx context.Context, in incoming, messages *orderMessages) error {
	if handled, err := b.adminMessagePageCallback(ctx, in, messages); handled {
		return err
	}
	parts := strings.Split(in.text, ":")
	if len(parts) == 3 && parts[1] == "results" {
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return err
		}
		return b.sendAdminMessagePage(ctx, in, id, 0, messages)
	}
	text, id, err := b.adminMessageCallback(ctx, in, messages)
	if err != nil {
		return err
	}
	return b.sendAdminMessageView(ctx, in.chat, text, id, messages)
}
func adminDestination(destination adminmessage.Destination) string {
	if destination.Thread > 0 {
		return fmt.Sprintf("%s:%d", destination.Chat, destination.Thread)
	}
	return destination.Chat
}

func (b *Bot) adminMessageCallback(ctx context.Context, in incoming, messages *orderMessages) (string, int64, error) {
	parts := strings.Split(in.text, ":")
	if len(parts) != callbackParts {
		return messages.text(i18n.AdminMessageHelp, nil), 0, nil
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", 0, &core.ProblemError{Status: http.StatusBadRequest, Code: "admin_message_invalid"}
	}
	if id <= 0 {
		return messages.text(i18n.AdminMessageHelp, nil), 0, nil
	}
	path := "/v1/admin-messages/" + parts[2] + "/" + parts[1]
	switch parts[1] {
	case botPhaseSend, mediaCancel:
		var result struct {
			OK bool `json:"ok"`
		}
		err = b.API.Call(ctx, in.owner, http.MethodPost, path, nil, &result)
		notice := i18n.AdminMessageQueued
		if parts[1] == mediaCancel {
			notice = i18n.AdminMessageCancelled
		}
		return messages.text(notice, map[string]string{"id": parts[2]}), id, err
	case "inputcancel":
		var result struct {
			OK bool `json:"ok"`
		}
		err = b.API.Call(
			ctx,
			in.owner,
			http.MethodPost,
			"/v1/admin-messages/input/cancel",
			map[string]int64{"id": id},
			&result,
		)
		return messages.text(i18n.AdminMessageCancelled, map[string]string{"id": parts[2]}), 0, err
	default:
		return messages.text(i18n.AdminMessageHelp, nil), 0, nil
	}
}

// Content and large recipient snapshots are split before the final action card,
// so Telegram's message limit never hides part of the reviewed request.
func (b *Bot) sendAdminMessageView(
	ctx context.Context,
	chat int64,
	text string,
	id int64,
	messages *orderMessages,
) error {
	const chunkRunes = 1800
	runes := []rune(text)
	index := 0
	ref := botdelivery.Reference{Family: botFamilyAdminView}
	if id > 0 {
		ref.Family = botFamilyAdminPage
		ref.Version = id
	}
	for len(runes) > chunkRunes {
		if err := b.queueBotUpdateResult(
			ctx,
			chat,
			fmt.Sprintf("admin_view:%d:%d", id, index),
			ref,
			botdelivery.StoredResult{Payload: telegram.Send{ChatID: chat, Text: string(runes[:chunkRunes])}},
		); err != nil {
			return err
		}
		runes = runes[chunkRunes:]
		index++
	}
	payload := telegram.Send{ChatID: chat, Text: string(runes)}
	if id > 0 {
		for _, action := range []struct {
			name  string
			label i18n.ID
		}{{botPhaseSend, i18n.AdminMessageSend}, {"results", i18n.AdminMessageResults}, {mediaCancel, i18n.AdminMessageCancel}} {
			payload.Markup.Rows = append(
				payload.Markup.Rows,
				[]telegram.Button{
					{
						Text: messages.text(action.label, nil),
						Data: fmt.Sprintf("%s%s:%d", adminMessagePrefix, action.name, id),
					},
				},
			)
		}
	}
	if messages.err != nil {
		return messages.err
	}
	return b.queueBotUpdateResult(
		ctx,
		chat,
		fmt.Sprintf("admin_view:%d:%d", id, index),
		ref,
		botdelivery.StoredResult{Payload: payload},
	)
}
