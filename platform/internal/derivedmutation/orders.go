package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// ExecuteOrder receives only host-bound derivation, never public command JSON.
// Provider verification and model work must finish before this transaction.
func (s Service) ExecuteOrder(
	ctx context.Context,
	actor string,
	command orders.Command,
	source readsource.Derivation,
) (orders.Order, error) {
	if !source.Valid() {
		return orders.Order{}, invalidSource()
	}
	source = source.Clone()
	if command.Origin != agentOrigin {
		return orders.Order{}, invalidSource()
	}
	if command.Name == "create" || command.Name == "edit" {
		if command.HistoryGeneration == nil || *command.HistoryGeneration != *source.Generation {
			return orders.Order{}, invalidSource()
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return orders.Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return orders.Order{}, err
	}
	if err = orders.LockEvent(ctx, tx, command.EventID); err != nil {
		return orders.Order{}, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, source.Authorities); err != nil {
		return orders.Order{}, err
	}
	prepared, err := s.Orders.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return orders.Order{}, err
	}
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return orders.Order{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return orders.Order{}, err
	}
	return result, tx.Commit(ctx)
}
