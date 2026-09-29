package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (b *Bot) loadPassOperation(
	ctx context.Context,
	owner, id string,
) (*agenthost.ScriptPassRequest, *readsource.Derivation, error) {
	if err := (derivedmutation.PassOperationQuery{ID: id}).Validate(); err != nil || id == "" {
		return nil, nil, errors.New("invalid pass operation reference")
	}
	request, source, err := b.scriptHost().Store.ReadRegistrationOperation(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	if err = b.authorizePassRequest(ctx, owner, request); err != nil {
		return nil, nil, err
	}
	return request, source, nil
}

func (b *Bot) passOperations(ctx context.Context, owner string, query derivedmutation.PassOperationQuery) (any, error) {
	return b.registrationOperations().Read(ctx, owner, query)
}

func (b *Bot) registrationOperations() interaction.RegistrationOperations {
	return interaction.RegistrationOperations{Ledger: b.scriptHost().Store, Domain: b.Host}
}
