package bot

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) loadAdminMessagePage(ctx context.Context, owner string, id, offset int64) (adminmessage.Page, error) {
	var page adminmessage.Page
	err := b.API.call(
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
	page, err := b.loadAdminMessagePage(ctx, in.owner, id, offset)
	if err == nil && page.State == broadcastPreparing {
		var resumed adminmessage.Message
		err = b.API.call(ctx, in.owner, http.MethodPost, fmt.Sprintf("/v1/admin-messages/%d/resume", id), nil, &resumed)
		if err == nil {
			page, err = b.loadAdminMessagePage(ctx, in.owner, id, offset)
		}
	}
	if err != nil {
		return err
	}
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
		_, sendErr := b.TG.Send(ctx, telegram.Send{ChatID: in.chat, Text: text, Markup: telegram.Markup{Rows: rows}})
		return sendErr
	}
	if messages.err != nil {
		return messages.err
	}
	if _, err = b.TG.Send(
		ctx,
		telegram.Send{ChatID: in.chat, Text: text, Markup: telegram.Markup{Rows: rows}},
	); err != nil {
		return err
	}
	if len(page.Items) > 0 {
		text += "\n\n" + page.Items[0].Content.Text
	}
	return b.sendAdminMessageView(ctx, in.chat, text, id, messages)
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
	page, err := b.loadAdminMessagePage(ctx, in.owner, id, offset)
	if err == nil && page.State == broadcastPreparing {
		var resumed adminmessage.Message
		err = b.API.call(ctx, in.owner, http.MethodPost, fmt.Sprintf("/v1/admin-messages/%d/resume", id), nil, &resumed)
		if err == nil {
			page, err = b.loadAdminMessagePage(ctx, in.owner, id, offset)
		}
	}
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
	return true, b.sendAdminMessageView(ctx, in.chat, adminDestination(item.Destination)+"\n"+text, id, messages)
}
