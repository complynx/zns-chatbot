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

func (b *Bot) beginAdminMessageInput(
	ctx context.Context,
	in incoming,
	raw, key string,
	messages *orderMessages,
) error {
	var input adminmessage.Input
	err := b.API.Call(ctx, in.owner, http.MethodPost, "/v1/admin-messages/input/start", struct {
		Key     string `json:"key"`
		Command string `json:"command"`
		ChatID  int64  `json:"chat_id"`
	}{key, raw, in.chat}, &input)
	if err != nil {
		return err
	}
	return b.sendAdminInputPrompt(ctx, in, input, messages)
}

func (b *Bot) sendAdminInputPrompt(
	ctx context.Context,
	in incoming,
	input adminmessage.Input,
	messages *orderMessages,
) error {
	if input.PromptID != 0 || input.State != broadcastPending {
		return nil
	}
	label := i18n.AdminBroadcastInput
	if input.Forward {
		label = i18n.AdminBroadcastForward
	}
	ref := botdelivery.Reference{
		Family:       botFamilyAdminPrompt,
		Version:      input.ID,
		Continuation: botdelivery.Continuation{Kind: botFamilyAdminPrompt, ID: input.ID},
	}
	payload := telegram.Send{
		ChatID: in.chat,
		Text:   messages.text(label, nil),
		Markup: telegram.Markup{
			Rows: [][]telegram.Button{
				{
					{
						Text: messages.text(i18n.AdminMessageCancel, nil),
						Data: fmt.Sprintf("adminmsg:inputcancel:%d", input.ID),
					},
				},
			},
		},
	}
	if messages.err != nil {
		return messages.err
	}
	return b.queueBotResult(
		ctx,
		in.owner,
		in.chat,
		0,
		"admin_prompt:"+strconv.FormatInt(input.ID, 10),
		ref,
		botdelivery.StoredResult{Notice: label, Payload: payload}, 0,
	)
}

// Only an explicit reply to our authoritative prompt or the legacy cancel marker
// enters this path. A pending broadcast never captures unrelated free text/media.
func (b *Bot) handleAdminMessageInput(ctx context.Context, in incoming, u telegram.Update) (bool, error) {
	if u.Message == nil {
		return false, nil
	}
	cancel := strings.HasPrefix(u.Message.Text, "\U000E007F")
	if u.Message.ReplyToMessage == nil && !cancel {
		return false, nil
	}
	var pending []adminmessage.Input
	err := b.API.Call(
		ctx,
		in.owner,
		http.MethodPost,
		"/v1/admin-messages/input/pending",
		map[string]int64{broadcastChatKey: in.chat},
		&pending,
	)
	if core.IsDatabaseFailure(err) {
		return false, core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var selected *adminmessage.Input
	for i := range pending {
		if cancel || (u.Message.ReplyToMessage != nil && u.Message.ReplyToMessage.ID == pending[i].PromptID) {
			selected = &pending[i]
			break
		}
	}
	if selected == nil {
		return false, nil
	}
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return true, err
	}
	messages := &orderMessages{language: prefs.Language}
	if cancel {
		var result struct {
			OK bool `json:"ok"`
		}
		err = b.API.Call(
			ctx,
			in.owner,
			http.MethodPost,
			"/v1/admin-messages/input/cancel",
			map[string]int64{"id": selected.ID},
			&result,
		)
		if err != nil {
			return true, err
		}
		return true, b.sendAdminMessageView(
			ctx,
			in.chat,
			messages.text(i18n.AdminMessageCancelled, map[string]string{"id": strconv.FormatInt(selected.ID, 10)}),
			0,
			messages,
		)
	}
	key := fmt.Sprintf("tg-admin-attach-%d", u.ID)
	if err = b.registerAdminMessageSource(ctx, in.owner, key, *u.Message); err != nil {
		return true, err
	}
	var preview adminmessage.Message
	err = b.API.Call(
		ctx,
		in.owner,
		http.MethodPost,
		"/v1/admin-messages/input/attach",
		adminmessage.Attachment{
			InputID:  selected.ID,
			ChatID:   in.chat,
			PromptID: selected.PromptID,
			Key:      key,
		},
		&preview,
	)
	if err != nil {
		return true, err
	}
	return true, b.sendAdminMessagePage(ctx, in, preview.ID, 0, messages)
}
