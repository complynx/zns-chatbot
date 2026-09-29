package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) SetCreditPolicy(
	ctx context.Context,
	actor, payer string,
	input credits.PolicyChange,
	source readsource.Derivation,
) (credits.Policy, error) {
	if !source.Valid() {
		return credits.Policy{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.beginSourceMutation(ctx, []string{actor, payer}, source)
	if err != nil {
		return credits.Policy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Credits.PreparePolicyInTx(ctx, tx, actor, payer, input)
	if err != nil {
		return credits.Policy{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}
