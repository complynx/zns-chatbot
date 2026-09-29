package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// ExecuteMassage fences host-bound source evidence in the domain effect transaction.
func (s Service) ExecuteMassage(
	ctx context.Context,
	actor string,
	command massage.Command,
	source readsource.Derivation,
) (massage.Reservation, error) {
	if !source.Valid() {
		return massage.Reservation{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return massage.Reservation{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return massage.Reservation{}, err
	}

	if err = readsource.LockActors(ctx, tx, []string{actor}, source.Authorities); err != nil {
		return massage.Reservation{}, err
	}
	prepared, err := s.Massage.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return massage.Reservation{}, err
	}
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return massage.Reservation{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return massage.Reservation{}, err
	}
	return result, tx.Commit(ctx)
}

// SetMassagePreferences preserves practitioner authorization and source locks through the update.
func (s Service) SetMassagePreferences(
	ctx context.Context,
	actor, event string,
	value massage.Preferences,
	source readsource.Derivation,
) (massage.Preferences, error) {
	if !source.Valid() {
		return massage.Preferences{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return massage.Preferences{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return massage.Preferences{}, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, source.Authorities); err != nil {
		return massage.Preferences{}, err
	}
	prepared, err := s.Massage.PreparePreferencesInTx(ctx, tx, actor, event, value)
	if err != nil {
		return massage.Preferences{}, err
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return massage.Preferences{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return massage.Preferences{}, err
	}
	return result, tx.Commit(ctx)
}
