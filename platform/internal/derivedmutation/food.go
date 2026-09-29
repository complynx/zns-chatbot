package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// ExecuteFood fences host-bound source evidence in the domain effect transaction.
func (s Service) ExecuteFood(
	ctx context.Context,
	actor string,
	command legacyfood.Command,
	source readsource.Derivation,
) (legacyfood.Order, error) {
	if !source.Valid() {
		return legacyfood.Order{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return legacyfood.Order{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return legacyfood.Order{}, err
	}
	if err = s.Food.LockEvent(ctx, tx, command.EventID); err != nil {
		return legacyfood.Order{}, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, source.Authorities); err != nil {
		return legacyfood.Order{}, err
	}
	prepared, err := s.Food.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return legacyfood.Order{}, err
	}
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return legacyfood.Order{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return legacyfood.Order{}, err
	}
	return result, tx.Commit(ctx)
}
