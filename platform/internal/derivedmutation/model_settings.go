package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) SetModelSettings(
	ctx context.Context,
	actor, scope string,
	input modelsettings.Change,
	source readsource.Derivation,
) (modelsettings.State, error) {
	if !source.Valid() {
		return modelsettings.State{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.beginSourceMutation(ctx, []string{actor, scope}, source)
	if err != nil {
		return modelsettings.State{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.ModelSettings.PrepareSetInTx(ctx, tx, actor, scope, input)
	if err != nil {
		return modelsettings.State{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}

func (s Service) GrantModelSettings(
	ctx context.Context,
	actor string,
	input modelsettings.Grant,
	source readsource.Derivation,
) error {
	if !source.Valid() || input.OperationKey == "" {
		return invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor, input.Owner}, source.Authorities); err != nil {
		return err
	}
	prepared, err := s.ModelSettings.PrepareGrantInTx(ctx, tx, actor, input)
	if err != nil {
		return err
	}
	if prepared.Replay() {
		return nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return err
	}
	if err = prepared.Apply(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
