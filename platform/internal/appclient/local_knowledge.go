package appclient

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// LocalKnowledge shares domain transactions and live authorization with HTTP.
// Installing it on Client does not expose the Host-only operations.
type LocalKnowledge struct {
	Service    knowledge.Service
	Authorizer applicationauth.Authorizer
}

func directKnowledge[T any](ctx context.Context, c Client, owner string,
	operation func(knowledge.Service, string) (T, error),
) (T, error) {
	return authenticatedKnowledge(ctx, c.LocalKnowledge, c.UserToken, owner, http.StatusForbidden, operation)
}

func hostKnowledge[T any](ctx context.Context, c Host, owner string,
	operation func(knowledge.Service, string) (T, error),
) (T, error) {
	return authenticatedKnowledge(ctx, c.LocalKnowledge, c.UserToken, owner, http.StatusUnauthorized, operation)
}

func authenticatedKnowledge[T any](ctx context.Context, local *LocalKnowledge,
	tokenFor func(context.Context, string) (string, error), owner string, mismatchStatus int,
	operation func(knowledge.Service, string) (T, error),
) (T, error) {
	var zero T
	if tokenFor == nil {
		return zero, identity.ErrZitadelIdentity
	}
	token, err := tokenFor(ctx, owner)
	if err != nil {
		if errors.Is(err, identity.ErrZitadelIdentity) || errors.Is(err, identity.ErrZitadelUserInactive) {
			return zero, err
		}
		return zero, orderApplicationError(err)
	}
	actor, err := authorizeOwnerStatus(ctx, local.Authorizer, token, owner, mismatchStatus)
	if err != nil {
		return zero, err
	}
	value, err := operation(local.Service, actor)
	if err != nil {
		return zero, knowledgeApplicationError(err)
	}
	// Apply the same bounded union check, retaining exact per-item domain evidence.
	// Local calls do not serialize or decode a wire envelope.
	envelope, err := knowledge.EvidenceEnvelope(value)
	if err != nil {
		return zero, knowledgeApplicationError(err)
	}
	// Page-level consumers need the union as well as the exact item evidence.
	refs := knowledgeEnvelopeAuthorities(envelope)
	switch target := any(&value).(type) {
	case *knowledge.FactPage:
		target.ReadAuthorities = refs
	case *knowledge.MemoryPage:
		target.ReadAuthorities = refs
	case *knowledge.MemoryOverview:
		target.ReadAuthorities = refs
	case *knowledge.Result:
		target.ReadAuthorities = refs
	case *conversation.Page:
		target.ReadAuthorities = refs
	}
	return value, nil
}

func knowledgeApplicationError(err error) error {
	if errors.Is(err, readsource.ErrLimit) {
		return &core.ProblemError{Status: http.StatusUnprocessableEntity, Code: "source_authority_limit"}
	}
	return orderApplicationError(err)
}

func knowledgeEnvelopeAuthorities(envelope any) []readsource.Authority {
	switch value := envelope.(type) {
	case knowledge.FactPage:
		return value.ReadAuthorities
	case knowledge.MemoryPage:
		return value.ReadAuthorities
	case knowledge.MemoryOverview:
		return value.ReadAuthorities
	case knowledge.Result:
		return value.ReadAuthorities
	case conversation.Page:
		return value.ReadAuthorities
	default:
		return nil
	}
}
