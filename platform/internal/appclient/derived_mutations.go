package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// LocalDerived uses the same live user authorizer as the HTTP boundary.
type LocalDerived struct {
	Service    derivedmutation.Service
	Authorizer applicationauth.Authorizer
}

const maxDerivedOrderCommandBytes = 320 << 10

func (c Host) ExecuteDerivedOrder(
	ctx context.Context,
	owner string,
	command orders.Command,
	source readsource.Derivation,
) (orders.Order, error) {
	if c.UserToken == nil {
		return orders.Order{}, identity.ErrZitadelIdentity
	}
	if err := validateDerivedCommandLimit(command, source, maxDerivedOrderCommandBytes); err != nil {
		return orders.Order{}, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (orders.Order, error) {
				return s.ExecuteOrder(ctx, actor, command, source.Clone())
			}),
		)
	}
	var result orders.Order
	err := c.derivedRequest(ctx, owner, "/internal/derived/order-actions", command, source, &result)
	return result, err
}

func (c Host) ExecuteDerivedWorkflow(
	ctx context.Context,
	owner string,
	command workflow.Action,
	source readsource.Derivation,
) (workflow.Workflow, error) {
	if c.UserToken == nil {
		return workflow.Workflow{}, identity.ErrZitadelIdentity
	}
	if err := validateDerivedCommand(command, source); err != nil {
		return workflow.Workflow{}, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (workflow.Workflow, error) {
				return s.ExecuteWorkflow(ctx, actor, command, source.Clone())
			}),
		)
	}
	var result workflow.Workflow
	err := c.derivedRequest(ctx, owner, "/internal/derived/actions", command, source, &result)
	return result, err
}

func (c Host) ExecuteDerivedKnowledge(
	ctx context.Context,
	owner string,
	command knowledge.Command,
	source readsource.Derivation,
) (knowledge.Result, error) {
	if c.LocalKnowledge != nil {
		return hostKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Result, error) {
			return s.ExecuteDerived(ctx, actor, command, source.Clone())
		})
	}

	var result knowledge.Result
	err := c.derivedRequest(ctx, owner, "/internal/knowledge/derived", command, source, &knowledgeEnvelope{&result})
	result.RestoreReadAuthorities()
	return result, err
}

func directDerived[T any](
	ctx context.Context,
	c Host,
	owner string,
	operation func(derivedmutation.Service, string) (T, error),
) (T, error) {
	var zero T
	if c.UserToken == nil {
		return zero, identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return zero, err
	}
	actor, err := authorizeOwnerStatus(ctx, c.LocalDerived.Authorizer, token, owner, http.StatusUnauthorized)
	if err != nil {
		return zero, err
	}
	result, err := operation(c.LocalDerived.Service, actor)
	if err != nil {
		return zero, orderApplicationError(err)
	}
	return result, nil
}

func (c Host) derivedRequest(
	ctx context.Context,
	owner, path string,
	command any,
	source readsource.Derivation,
	out any,
) error {
	if c.UserToken == nil {
		return identity.ErrZitadelIdentity
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		Command any                   `json:"command"`
		Source  readsource.Derivation `json:"source"`
	}{command, source.Clone()})
	if err != nil {
		return err
	}
	limit := int64(MaxAPIBytes)
	if _, ok := out.(*knowledgeEnvelope); ok {
		limit = knowledgeResponseBytes
	}
	return requestAuthorizedLimit(ctx, c.Base, c.HTTP,
		token,
		c.Signer.DerivedMutationToken(owner),
		http.MethodPost,
		path,
		body,
		out,
		limit,
	)
}
