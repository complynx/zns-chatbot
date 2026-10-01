package derivedmutation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// RunPassBatch binds host derivation once. Resumption always uses the stored
// source even if another model turn supplies a newer history generation.
func (s Service) RunPassBatch(
	ctx context.Context,
	actor string,
	c passbooking.RuntimeBatch,
	source readsource.Derivation,
) ([]passbooking.RuntimeBatchItem, error) {
	if !source.Valid() {
		return nil, invalidSource()
	}
	source = source.Clone()
	return s.runPassBatch(ctx, actor, c, &source)
}

// RunManualPassBatch allows manual continuation without stripping the source
// of a batch originally prepared by an agent.
func (s Service) RunManualPassBatch(
	ctx context.Context,
	actor string,
	c passbooking.RuntimeBatch,
) ([]passbooking.RuntimeBatchItem, error) {
	return s.runPassBatch(ctx, actor, c, nil)
}

func decodeBatchSource(raw json.RawMessage) (*readsource.Derivation, error) {
	var source readsource.Derivation
	if err := json.Unmarshal(raw, &source); err != nil || !source.Valid() {
		return nil, invalidSource()
	}
	return &source, nil
}

func lockBatchPrelude(
	ctx context.Context,
	tx pgx.Tx,
	event string,
	actors []string,
	source *readsource.Derivation,
) error {
	refs := []readsource.Authority{}
	if source != nil {
		refs = source.Authorities
	}
	if err := readsource.LockRegistrationMutationEvents(ctx, tx, refs, []string{event}); err != nil {
		return err
	}
	return readsource.LockActors(ctx, tx, actors, refs)
}

func (s Service) preparePassBatch(
	ctx context.Context,
	actor string,
	c passbooking.RuntimeBatch,
	source *readsource.Derivation,
) error {
	existing, err := s.Registration.ReadRuntimeBatch(ctx, actor, c)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	actors := []string{actor}
	if existing != nil {
		source = nil
		if len(existing.Source) > 0 {
			source, err = decodeBatchSource(existing.Source)
			if err != nil {
				return err
			}
		}
		actors = existing.Actors()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockBatchPrelude(ctx, tx, c.Event, actors, source); err != nil {
		return err
	}
	batch, found, err := s.Registration.PrepareRuntimeBatchInTx(ctx, tx, actor, c)
	if err != nil {
		return err
	}
	if found {
		return tx.Commit(ctx)
	}
	var raw json.RawMessage
	if source != nil {
		if err = lockSource(ctx, tx, actor, *source); err != nil {
			return err
		}
		raw, err = json.Marshal(source)
		if err != nil {
			return err
		}
	}
	if err = batch.Persist(ctx, tx, c, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) runPassBatch(
	ctx context.Context,
	actor string,
	c passbooking.RuntimeBatch,
	source *readsource.Derivation,
) ([]passbooking.RuntimeBatchItem, error) {
	if err := s.preparePassBatch(ctx, actor, c, source); err != nil {
		return nil, err
	}
	var items []passbooking.RuntimeBatchItem
	for index := range c.Recipients {
		var err error
		items, err = s.runPassBatchItem(ctx, actor, c, index)
		if err != nil {
			return items, err
		}
	}
	return items, nil
}

func (s Service) runPassBatchItem(
	ctx context.Context,
	actor string,
	c passbooking.RuntimeBatch,
	index int,
) ([]passbooking.RuntimeBatchItem, error) {
	if err := s.Registration.ResolveRegistrationIntake(ctx, c.Event); err != nil {
		return nil, err
	}
	batch, err := s.Registration.ReadRuntimeBatch(ctx, actor, c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalidSource()
	}
	if err != nil {
		return nil, err
	}
	var source *readsource.Derivation
	if len(batch.Source) > 0 {
		source, err = decodeBatchSource(batch.Source)
		if err != nil {
			return nil, err
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	registration, clockAttempt := s.Registration.WithClockAttempt()
	s.Registration = registration
	if err = lockBatchPrelude(ctx, tx, c.Event, batch.Actors(), source); err != nil {
		return nil, err
	}
	if err = batch.LockRuntimeBatchInTx(ctx, tx); err != nil {
		return nil, err
	}
	if !batch.Pending(index) {
		return batch.Items(), tx.Commit(ctx)
	}
	committedItems := slices.Clone(batch.Items())
	err = s.applyPassBatchItem(ctx, tx, actor, batch, index, source)
	if saveErr := batch.SaveOutcome(ctx, tx, index, err); saveErr != nil {
		return batch.Items(), saveErr
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return committedItems, err
	}
	return batch.Items(), tx.Commit(ctx)
}

func (s Service) applyPassBatchItem(
	ctx context.Context,
	outer pgx.Tx,
	actor string,
	batch *passbooking.RuntimeBatchState,
	index int,
	source *readsource.Derivation,
) error {
	tx, err := outer.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Registration.PrepareRuntimeBatchItem(ctx, tx, batch, index)
	if err != nil {
		return err
	}
	if prepared.Replayed() {
		return tx.Commit(ctx)
	}
	if source != nil {
		if err = lockBatchSource(ctx, tx, actor, batch, index, *source); err != nil {
			return err
		}
	}
	if err = prepared.Apply(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
