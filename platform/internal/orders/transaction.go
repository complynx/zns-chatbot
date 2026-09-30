package orders

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"

	"github.com/jackc/pgx/v5"
)

// LockEvent lets application orchestration include the target event in its
// ordered source/target prelude. Authorization remains in PrepareInTx.
func LockEvent(ctx context.Context, tx pgx.Tx, event string) error {
	_, err := tx.Exec(ctx, `SELECT id FROM core.order_events WHERE id=$1 FOR UPDATE`, event)
	return core.DatabaseOperationError(err)
}

// PreparedCommand retains authorized command state inside its caller's transaction.
// It permits the application to fence causal sources after receipt replay and
// before a new effect without duplicating domain authorization or SQL.
type PreparedCommand struct {
	op     operation
	hash   string
	replay Order
	found  bool
}

func (s Service) PrepareInTx(ctx context.Context, tx pgx.Tx, actor string, c Command) (*PreparedCommand, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	p := &PreparedCommand{op: operation{tx: tx, actor: actor, command: c, deliveryBotID: s.Delivery.BotID}}
	if err := p.op.authorize(ctx); err != nil {
		return nil, err
	}
	body, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	p.hash = hex.EncodeToString(digest[:])
	p.replay, p.found, err = p.op.replay(ctx, p.hash)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PreparedCommand) Replay() (Order, bool) { return p.replay, p.found }

// Apply keeps all effects and the command receipt in the prepared transaction.
// The caller owns commit; a replay performs no writes.
func (p *PreparedCommand) Apply(ctx context.Context) (Order, error) {
	if p.found {
		return p.replay, nil
	}
	op := &p.op
	c, tx, actor := op.command, op.tx, op.actor
	// A committed receipt survives deletion; only a new derived mutation needs
	// the history fence, held through state and receipt commit.
	if err := fence.LockGeneration(ctx, tx, actor, c.HistoryGeneration); err != nil {
		return Order{}, err
	}
	if c.CatalogSnapshot != "" && c.CatalogSnapshot != CatalogSnapshot(op.event) {
		return Order{}, problem(http.StatusConflict, "catalog_changed")
	}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&op.now); err != nil {
		return Order{}, core.DatabaseOperationError(err)
	}
	if !c.isAdmin() && c.Name != actionCountry && !op.now.Before(op.event.Deadline) {
		return Order{}, problem(http.StatusConflict, "deadline")
	}
	order, err := op.loadOrder(ctx)
	if err != nil {
		return Order{}, err
	}
	op.before = order.Choice
	op.before.Extras = maps.Clone(order.Choice.Extras)
	op.previous = order
	if err = ensureCapacitySlots(ctx, tx, op.event); err != nil {
		return Order{}, err
	}
	if err = op.apply(ctx, &order); err != nil {
		return Order{}, err
	}
	return op.persist(ctx, order, p.hash)
}
