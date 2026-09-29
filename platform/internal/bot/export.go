package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

const actionExport = "export"

func (b *Bot) exportOrders(ctx context.Context, in incoming, update int64) (string, error) {
	return b.exportOrdersWithSource(ctx, in, update, nil)
}

func (b *Bot) exportOrdersWithSource(
	ctx context.Context,
	in incoming,
	update int64,
	source *readsource.Derivation,
) (string, error) {
	if err := b.rememberOrderLocale(ctx, in.owner, update); err != nil {
		return "", err
	}
	if err := b.checkOrderDeliverySource(ctx, in.owner, source); err != nil {
		return "", err
	}
	observed, err := b.queueBotDocument(
		ctx,
		in.owner,
		in.chat,
		botdelivery.Reference{
			Family:       botFamilyOrderExport,
			Event:        b.currentOrderEvent(),
			Update:       update,
			Source:       source,
			Notice:       i18n.OrderExported,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind, Key: botFamilyOrderExport},
		},
	)
	if err != nil {
		return "", err
	}
	if documentDelivered(observed) {
		return b.orderMessage(ctx, in.owner, i18n.OrderExported, nil)
	}
	return "", nil
}
