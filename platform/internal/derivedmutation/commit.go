package derivedmutation

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type preparedMutation[T any] interface {
	Replay() (T, bool)
	Apply(context.Context) (T, error)
}

// beginSourceMutation locks source events before affected actors. Domain-specific
// event upgrades, such as registration, use their own prelude instead.
func (s Service) beginSourceMutation(
	ctx context.Context,
	actors []string,
	source readsource.Derivation,
) (pgx.Tx, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err == nil {
		err = readsource.LockActors(ctx, tx, actors, source.Authorities)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// commitPrepared follows domain preparation, including current target rights.
// A committed receipt can replay; a new effect holds its source fence to commit.
func commitPrepared[T any](
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	source readsource.Derivation,
	prepared preparedMutation[T],
) (T, error) {
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	var zero T
	if err := lockSource(ctx, tx, actor, source); err != nil {
		return zero, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return zero, err
	}
	return result, tx.Commit(ctx)
}
