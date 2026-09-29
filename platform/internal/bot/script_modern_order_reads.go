package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

type modernOrderReadChunk struct {
	core.ReadChunk

	Offset int `json:"offset"`
}

func modernOrderFingerprint(order orders.Order) (string, error) {
	return orders.OrderSnapshot(order)
}

// Successful script-call receipts form the durable page chain. Pending calls,
// fabricated cursors and another actor/event/order's receipts cannot advance it.
// Resume replays the last acknowledged page, so an interrupted delivery cannot
// silently skip a page. Current domain rights and snapshot are checked afterward.
func (b *Bot) modernOrderReadCheckpoint(
	ctx context.Context,
	owner, name, event string,
	args modernOrderArguments,
) (agenthost.ModernOrderRequest, error) {
	var checkpoint agenthost.ModernOrderRequest
	if args.Cursor == "" && !args.Resume {
		return checkpoint, nil
	}
	err := b.DB.QueryRow(ctx, `SELECT call.value->'modern_order' FROM bot.interactions i
	 CROSS JOIN LATERAL jsonb_array_elements(i.content) WITH ORDINALITY run(value,position)
	 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(run.value->'calls','[]'::jsonb)) WITH ORDINALITY call(value,position)
	 WHERE i.owner=$1 AND i.kind=$2 AND call.value->'outcome'->>'name'=$3
	 AND COALESCE(call.value->'outcome'->>'error','')='' AND call.value->'outcome' ? 'result'
	 AND call.value->'modern_order'->>'event'=$4 AND call.value->'modern_order'->>'read_order_id'=$5
	 AND COALESCE(call.value->'modern_order'->>'read_snapshot','')<>''
	 AND ($6 OR call.value->'outcome'->'result'->>'next_cursor'=$7)
	 ORDER BY i.id DESC,run.position DESC,call.position DESC LIMIT 1`, owner, scriptRunsKind, name, event, args.OrderID, args.Resume, args.Cursor).Scan(&checkpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return checkpoint, errors.New("order continuation unavailable; restart read")
	}
	return checkpoint, err
}
