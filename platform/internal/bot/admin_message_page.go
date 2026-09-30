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
		if messages.err != nil {
			return messages.err
		}
		if err = b.API.CheckAdminMessagePublication(ctx, in.owner, id); err != nil {
			return err
		}
		return b.queueAdminMessagePage(ctx, in.chat, id, offset, text, rows)
	}
	if messages.err != nil {
		return messages.err
	}
	if err = b.API.CheckAdminMessagePublication(ctx, in.owner, id); err != nil {
		return err
	}
	if err = b.queueAdminMessagePage(ctx, in.chat, id, offset, text, rows); err != nil {
		return err
	}
	if len(page.Items) > 0 {
		text += "\n\n" + page.Items[0].Content.Text
	}
	if err = b.API.CheckAdminMessagePublication(ctx, in.owner, id); err != nil {
		return err
	}
	return b.sendAdminMessageView(ctx, in.chat, text, id, messages)
}

// Split before outbound validation. Navigation remains on the final page chunk.
func (b *Bot) queueAdminMessagePage(
	ctx context.Context, chat, id, offset int64, text string, rows [][]telegram.Button,
) error {
	const chunkRunes = 1800
	runes := []rune(text)
	effect := fmt.Sprintf("admin_page:%d:%d", id, offset)
	ref := botdelivery.Reference{Family: botFamilyAdminPage, Version: id}
	for index := 0; len(runes) > chunkRunes; index++ {
		if err := b.queueBotUpdateResult(ctx, chat, fmt.Sprintf("%s:chunk:%d", effect, index), ref,
			botdelivery.StoredResult{Payload: telegram.Send{ChatID: chat, Text: string(runes[:chunkRunes])}},
		); err != nil {
			return err
		}
		runes = runes[chunkRunes:]
	}
	return b.queueBotUpdateResult(ctx, chat, effect, ref, botdelivery.StoredResult{
		Payload: telegram.Send{ChatID: chat, Text: string(runes), Markup: telegram.Markup{Rows: rows}},
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
