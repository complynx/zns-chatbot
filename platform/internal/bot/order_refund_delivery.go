package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const botFamilyRefund = "order_refund"
const botFamilyRefundRedaction = "order_refund_redaction"

func (b *Bot) deliverRefundCard(
	ctx context.Context,
	owner string,
	task orders.RefundTask,
	payload telegram.Send,
) error {
	binding := task.DeliveryRead()
	id := strconv.FormatInt(task.ID, 10)
	ref := botdelivery.Reference{
		Kind: botdelivery.CardIntent, Family: botFamilyRefund, Event: task.EventID,
		Object: id, CardKey: refundCardPrefix + id, Refund: &binding,
	}
	return b.deliverOrderCard(withBotCard(ctx, ref), owner, ref.CardKey, payload)
}

func (b *Bot) renderRefundRedaction(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	if !i.Reference.Continuation.Retired || i.Target <= 0 || i.Phase != botPhaseEdit {
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
	pref, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	text, err := i18n.Translate(pref.Language, i18n.OrderRetired, nil)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	payload := telegram.Send{
		ChatID: i.Chat, MessageID: i.Target, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}},
	}
	hash, err := botCardHash(payload)
	return botRenderedDelivery{Payload: payload, Receipt: botdelivery.Continuation{
		Kind: "order_card", Key: i.Reference.CardKey, ViewHash: hash, Retired: true,
	}}, err
}
