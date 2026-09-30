package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// MemoryReadState is an application operation, never a transport database fallback.
type MemoryReadState interface {
	ReserveKnowledge(context.Context, string, int64, agent.KnowledgeProposal) (int, error)
	ReconcileMemory(context.Context, string, knowledge.MemoryDeletionState) error
	CompleteKnowledge(
		context.Context,
		string,
		int64,
		int,
		agent.KnowledgeReadResult,
	) ([]agent.KnowledgeReadResult, error)
}

type LocalMemoryReadState struct {
	Service    MemoryReadState
	Authorizer applicationauth.Authorizer
}

type MemoryReadStateRequest struct {
	UpdateID int64                     `json:"update_id"`
	Index    int                       `json:"index"`
	Request  agent.KnowledgeProposal   `json:"request"`
	Result   agent.KnowledgeReadResult `json:"result"`
}

func (c Host) ReconcileMemory(ctx context.Context, owner string, state knowledge.MemoryDeletionState) error {
	_, err := memoryReadStateCall(ctx, c, owner, "reconcile", MemoryReadStateRequest{},
		func(s MemoryReadState) (struct{}, error) { return struct{}{}, s.ReconcileMemory(ctx, owner, state) })
	return err
}

func (c Host) ReserveKnowledge(
	ctx context.Context,
	owner string,
	updateID int64,
	request agent.KnowledgeProposal,
) (int, error) {
	return memoryReadStateCall(ctx, c, owner, "reserve", MemoryReadStateRequest{UpdateID: updateID, Request: request},
		func(s MemoryReadState) (int, error) { return s.ReserveKnowledge(ctx, owner, updateID, request) })
}

func (c Host) CompleteKnowledge(ctx context.Context, owner string, updateID int64, index int,
	result agent.KnowledgeReadResult,
) ([]agent.KnowledgeReadResult, error) {
	return memoryReadStateCall(
		ctx,
		c,
		owner,
		"complete",
		MemoryReadStateRequest{UpdateID: updateID, Index: index, Result: result},
		func(s MemoryReadState) ([]agent.KnowledgeReadResult, error) {
			return s.CompleteKnowledge(ctx, owner, updateID, index, result)
		},
	)
}

func memoryReadStateCall[T any](ctx context.Context, c Host, owner, action string, input MemoryReadStateRequest,
	local func(MemoryReadState) (T, error),
) (T, error) {
	var zero T
	if c.UserToken == nil || owner == "" {
		return zero, identity.ErrZitadelIdentity
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	if len(raw) > MaxAPIBytes {
		return zero, &core.ProblemError{Status: http.StatusRequestEntityTooLarge, Code: "memory_read_state_too_large"}
	}
	token, err := c.UserToken(ctx, owner)
	if err != nil {
		return zero, err
	}
	if c.LocalMemoryReadState != nil {
		if _, err = authorizeOwnerStatus(
			ctx,
			c.LocalMemoryReadState.Authorizer,
			token,
			owner,
			http.StatusUnauthorized,
		); err != nil {
			return zero, err
		}
		value, callErr := local(c.LocalMemoryReadState.Service)
		if callErr != nil {
			return zero, orderApplicationError(callErr)
		}
		return value, nil
	}
	var value T
	err = requestAuthorizedLimit(ctx, c.Base, c.HTTP, token, c.Signer.DerivedMutationToken(owner), http.MethodPost,
		"/internal/memory/read-state/"+action, raw, &value, MaxAPIBytes)
	return value, err
}
