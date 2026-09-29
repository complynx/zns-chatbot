package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) SetLanguage(
	ctx context.Context,
	actor string,
	input account.LanguageChange,
	source readsource.Derivation,
) (account.Preferences, error) {
	if !source.Valid() || input.OperationKey == "" || input.Initialize {
		return account.Preferences{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return account.Preferences{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = readsource.LockEvents(ctx, tx, source.Authorities); err != nil {
		return account.Preferences{}, err
	}
	if err = readsource.LockActors(ctx, tx, []string{actor}, source.Authorities); err != nil {
		return account.Preferences{}, err
	}
	prepared, err := s.Account.PrepareLanguageInTx(ctx, tx, actor, input)
	if err != nil {
		return account.Preferences{}, err
	}
	if value, found := prepared.Replay(); found {
		return value, nil
	}
	if err = lockSource(ctx, tx, actor, source); err != nil {
		return account.Preferences{}, err
	}
	value, err := prepared.Apply(ctx)
	if err != nil {
		return value, err
	}
	return value, tx.Commit(ctx)
}
