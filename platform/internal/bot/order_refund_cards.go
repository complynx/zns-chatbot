package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) renderRefunds(
	ctx context.Context,
	owner string,
	chat int64,
	language string,
	active map[string]bool,
) error {
	before, err := b.refundCursor(ctx, owner)
	if err != nil {
		return err
	}
	page, err := b.API.RefundTasks(ctx, owner, b.currentOrderEvent(), max(before, 0))
	if problem, ok := errors.AsType[*core.ProblemError](
		err,
	); ok && problem.Status == http.StatusForbidden &&
		!core.IsDatabaseFailure(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(page.Items) == 0 && before < 0 {
		return nil
	}
	messages := orderMessages{language: language}
	pager := telegram.Send{ChatID: chat, Text: messages.text(i18n.RefundList, nil)}
	if len(page.Items) == 0 {
		pager.Text = messages.text(i18n.RefundEmpty, nil)
	}
	if before > 0 {
		button, buttonErr := b.orderButton(
			ctx,
			owner,
			messages.text(i18n.RefundList, nil),
			orders.Command{Name: refundListAction},
		)
		if buttonErr != nil {
			return buttonErr
		}
		pager.Markup.Rows = append(pager.Markup.Rows, []telegram.Button{button})
	}
	if page.NextBefore > 0 {
		button, buttonErr := b.orderButton(ctx, owner, messages.text(i18n.PageNext, nil),
			orders.Command{Name: refundListAction, Version: page.NextBefore})
		if buttonErr != nil {
			return buttonErr
		}
		pager.Markup.Rows = append(pager.Markup.Rows, []telegram.Button{button})
	}
	if messages.err != nil {
		return messages.err
	}
	active[refundCardPrefix+"list"] = true
	if err = b.deliverOrderCard(ctx, owner, refundCardPrefix+"list", pager); err != nil {
		return err
	}
	for _, task := range page.Items {
		payload, payloadErr := b.refundPayload(ctx, owner, chat, language, task)
		if payloadErr != nil {
			return payloadErr
		}
		key := refundCardPrefix + strconv.FormatInt(task.ID, 10)
		active[key] = true
		if err = b.deliverRefundCard(ctx, owner, task, payload); err != nil {
			return err
		}
	}
	return nil
}

func refundText(language string, task orders.RefundTask) (string, error) {
	messages := orderMessages{language: language}
	amount, err := task.Amount.MarshalJSON()
	if err != nil {
		return "", err
	}
	text := messages.text(i18n.RefundSummary, map[string]string{
		"id": strconv.FormatInt(task.ID, 10), "order": task.OrderID, ownerField: task.Owner,
		"amount": messages.number(string(amount)), "currency": task.Currency,
	})
	text += orderDescription(orders.Order{Choice: orders.Choice{Extras: task.Extras}}, &messages)
	if task.State == "refunded" {
		text += "\n" + messages.text(i18n.RefundCompleted, nil)
	} else {
		text += "\n" + messages.text(i18n.RefundPending, nil)
		if task.RoutingReason != "" || task.Ambassador == "" {
			text += "\n" + messages.text(i18n.RefundUnassigned, nil)
		} else {
			text += "\n" + messages.text(i18n.RefundAmbassador, map[string]string{"ambassador": task.Ambassador})
		}
	}
	return text, messages.err
}

func (b *Bot) refundPayload(
	ctx context.Context,
	owner string,
	chat int64,
	language string,
	task orders.RefundTask,
) (telegram.Send, error) {
	text, err := refundText(language, task)
	payload := telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	if err != nil || !task.CanConfirm || task.State != "pending" {
		return payload, err
	}
	label, err := i18n.Translate(language, i18n.RefundConfirm, nil)
	if err != nil {
		return payload, err
	}
	button, err := b.orderButton(ctx, owner, label, orders.Command{
		Name:    refundConfirmAction,
		EventID: task.EventID,
		OrderID: strconv.FormatInt(task.ID, 10),
		Version: task.Version,
	})
	payload.Markup.Rows = append(payload.Markup.Rows, []telegram.Button{button})
	return payload, err
}
