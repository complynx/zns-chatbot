package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) ExecutePassProfile(
	ctx context.Context,
	actor string,
	command passes.Command,
	source readsource.Derivation,
) (passes.Profile, error) {
	if !source.Valid() || command.Origin != agentOrigin {
		return passes.Profile{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.beginSourceMutation(ctx, []string{actor}, source)
	if err != nil {
		return passes.Profile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Profile.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return passes.Profile{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}
