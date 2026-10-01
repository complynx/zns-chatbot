package bot

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) loadAdminMessagePage(ctx context.Context, owner string, id, offset int64) (adminmessage.Page, error) {
	var page adminmessage.Page
	err := b.API.Call(
		ctx,
		owner,
		http.MethodPost,
		"/v1/admin-messages/review",
		map[string]int64{"id": id, "offset": offset},
		&page,
	)
	return page, err
}

func (b *Bot) sendAdminMessagePage(ctx context.Context, in incoming, id, offset int64, messages *orderMessages) error {
	page, err := b.readyAdminMessagePage(ctx, in.owner, id, offset)
	if err != nil {
		return err
	}
	lines, rows := adminMessagePageItems(page, id, offset, messages)
	text := messages.text(
		i18n.AdminBroadcastPage,
		map[string]string{
			"id":              strconv.FormatInt(id, 10),
			orderTotalKey:     strconv.FormatInt(page.Total, 10),
			"from":            strconv.FormatInt(offset+1, 10),
			"to":              strconv.FormatInt(offset+int64(len(page.Items)), 10),
			broadcastItemsKey: strings.Join(lines, "\n"),
		},
	)
	text = adminMessageProgressText(page.Progress, messages) + "\n" + text
	if offset > 0 {
		rows = append(
			rows,
			[]telegram.Button{
				{
					Text: messages.text(i18n.AdminBroadcastPrevious, nil),
					Data: fmt.Sprintf("adminmsg:page:%d:%d", id, max(0, offset-broadcastPageSize)),
				},
			},
		)
	}
	if page.More {
		rows = append(
			rows,
			[]telegram.Button{
				{
					Text: messages.text(i18n.AdminBroadcastNext, nil),
					Data: fmt.Sprintf("adminmsg:page:%d:%d", id, offset+int64(len(page.Items))),
				},
			},
		)
	}
	if page.State == broadcastPreparing {
		text = messages.text(i18n.AdminBroadcastPreparing, nil) + "\n" + text
		rows = append(
			rows,
			[]telegram.Button{
				{Text: messages.text(i18n.AdminMessageResults, nil), Data: fmt.Sprintf("adminmsg:results:%d", id)},
				{Text: messages.text(i18n.AdminMessageCancel, nil), Data: fmt.Sprintf("adminmsg:cancel:%d", id)},
			},
		)
	}
	if messages.err != nil {
		return messages.err
	}
	if err = b.API.CheckAdminMessagePublication(ctx, in.owner, id); err != nil {
		return err
	}
	effects := adminMessagePageEffects(in.chat, id, offset, text, rows)
	if page.State != broadcastPreparing {
		if len(page.Items) > 0 {
			text += "\n\n" + page.Items[0].Content.Text
		}
		effects = append(effects, adminMessagePageViewEffects(in.chat, id, text, messages)...)
	}
	if messages.err != nil {
		return messages.err
	}
	origin, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || origin.owner == "" || origin.owner != origin.in.owner || origin.owner != in.owner || origin.in.chat != in.chat {
		return botdelivery.ErrBinding
	}
	return b.Host.EnqueueAdminPageResults(ctx, botdelivery.AdminPageResultsRequest{
		Owner: in.owner, Chat: in.chat, Update: origin.update.ID, ID: id, Offset: offset, Effects: effects,
	})
}

// Navigation keeps the original final effect identity. The host freezes all
// chunks and the action card together before registering any queue effects.
func adminMessagePageEffects(
	chat, id, offset int64,
	text string,
	rows [][]telegram.Button,
) []botdelivery.AdminPageEffect {
	const chunkRunes = 1800
	runes := []rune(text)
	final := fmt.Sprintf("admin_page:%d:%d", id, offset)
	var effects []botdelivery.AdminPageEffect
	for index := 0; len(runes) > chunkRunes; index++ {
		effects = append(effects, botdelivery.AdminPageEffect{
			Effect:  fmt.Sprintf("%s:chunk:%d", final, index),
			Payload: telegram.Send{ChatID: chat, Text: string(runes[:chunkRunes])},
		})
		runes = runes[chunkRunes:]
	}
	return append(effects, botdelivery.AdminPageEffect{
		Effect: final, Payload: telegram.Send{ChatID: chat, Text: string(runes), Markup: telegram.Markup{Rows: rows}},
	})
}

const adminMessageResultsAction = "results"

func adminMessagePageViewEffects(chat, id int64, text string, messages *orderMessages) []botdelivery.AdminPageEffect {
	const chunkRunes = 1800
	runes := []rune(text)
	var effects []botdelivery.AdminPageEffect
	for index := 0; len(runes) > chunkRunes; index++ {
		effects = append(effects, botdelivery.AdminPageEffect{
			Effect:  fmt.Sprintf("admin_view:%d:%d", id, index),
			Payload: telegram.Send{ChatID: chat, Text: string(runes[:chunkRunes])},
		})
		runes = runes[chunkRunes:]
	}
	payload := telegram.Send{ChatID: chat, Text: string(runes)}
	for _, action := range []struct {
		name  string
		label i18n.ID
	}{
		{botPhaseSend, i18n.AdminMessageSend}, {adminMessageResultsAction, i18n.AdminMessageResults}, {mediaCancel, i18n.AdminMessageCancel},
	} {
		payload.Markup.Rows = append(payload.Markup.Rows, []telegram.Button{{
			Text: messages.text(action.label, nil), Data: fmt.Sprintf("%s%s:%d", adminMessagePrefix, action.name, id),
		}})
	}
	return append(effects, botdelivery.AdminPageEffect{
		Effect: fmt.Sprintf("admin_view:%d:%d", id, len(effects)), Payload: payload,
	})
}
func adminMessageProgressText(progress adminmessage.JobProgress, messages *orderMessages) string {
	return messages.text(i18n.AdminMessageProgress, map[string]string{
		"succeeded":    strconv.FormatInt(progress.Succeeded, 10),
		"queued":       strconv.FormatInt(progress.Queued, 10),
		"deferred":     strconv.FormatInt(progress.Deferred, 10),
		"sending":      strconv.FormatInt(progress.Sending, 10),
		"refused":      strconv.FormatInt(progress.Rejected, 10),
		"cancelled":    strconv.FormatInt(progress.Cancelled, 10),
		"uncertain":    strconv.FormatInt(progress.Uncertain, 10),
		"parked":       strconv.FormatInt(progress.Parked, 10),
		"paused":       strconv.FormatInt(progress.Paused, 10),
		"sharedpaused": strconv.FormatInt(progress.SharedPaused, 10),
		"notqueued":    strconv.FormatInt(progress.NotQueued, 10),
	})
}

func (b *Bot) adminMessagePageCallback(ctx context.Context, in incoming, messages *orderMessages) (bool, error) {
	parts := strings.Split(in.text, ":")
	if len(parts) != 4 || (parts[1] != broadcastPageAction && parts[1] != "inspect") {
		return false, nil
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return true, err
	}
	offset, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return true, err
	}
	if parts[1] == broadcastPageAction {
		return true, b.sendAdminMessagePage(ctx, in, id, offset, messages)
	}
	page, err := b.readyAdminMessagePage(ctx, in.owner, id, offset)
	if err != nil {
		return true, err
	}
	if len(page.Items) == 0 {
		return true, nil
	}
	item := page.Items[0]
	text := item.Content.Text
	if item.Content.FromMessage != 0 {
		text = fmt.Sprintf("%d/%d", item.Content.FromChat, item.Content.FromMessage)
	}
	if err = b.API.CheckAdminMessagePublication(ctx, in.owner, id); err != nil {
		return true, err
	}
	return true, b.sendAdminMessageView(ctx, in.chat, adminDestination(item.Destination)+"\n"+text, id, messages)
}

func (b *Bot) readyAdminMessagePage(ctx context.Context, owner string, id, offset int64) (adminmessage.Page, error) {
	page, err := b.loadAdminMessagePage(ctx, owner, id, offset)
	if err != nil || page.State != broadcastPreparing {
		return page, err
	}
	var resumed adminmessage.Message
	if err = b.API.Call(
		ctx,
		owner,
		http.MethodPost,
		fmt.Sprintf("/v1/admin-messages/%d/resume", id),
		nil,
		&resumed,
	); err != nil {
		return page, err
	}
	return b.loadAdminMessagePage(ctx, owner, id, offset)
}

func adminMessagePageItems(
	page adminmessage.Page,
	id, offset int64,
	messages *orderMessages,
) ([]string, [][]telegram.Button) {
	var lines []string
	var rows [][]telegram.Button
	for i, item := range page.Items {
		status := item.State
		if status == "draft" || status == broadcastPreparing {
			status = broadcastPending
		}
		lines = append(
			lines,
			messages.text(
				i18n.AdminMessageResult,
				map[string]string{
					"destination": adminDestination(item.Destination),
					"status":      messages.text(i18n.ID("admin_message.state."+status), nil),
					"message":     strconv.FormatInt(item.TelegramMessageID, 10),
					"detail":      item.Failure,
				},
			),
		)
		rows = append(
			rows,
			[]telegram.Button{
				{
					Text: adminDestination(item.Destination),
					Data: fmt.Sprintf("adminmsg:inspect:%d:%d", id, offset+int64(i)),
				},
			},
		)
	}

	return lines, rows
}
