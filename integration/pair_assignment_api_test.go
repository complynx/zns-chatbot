package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const pairAssignAPI = "/internal/agent/pairs/assign"

func pairUserToken(f pairFixture, scope db.PaymentScope) string {
	f.H.peers.mu.Lock()
	defer f.H.peers.mu.Unlock()
	f.H.peers.adminRole = scope.GlobalAdmin
	return f.H.peers.token(scope.Subject)
}

func TestPairAssignmentAPIRequiresExactProposalAndReconcilesOriginal(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	adminToken, ownerToken := pairUserToken(f, admin), pairUserToken(f, f.A)
	prepare := map[string]any{"protocol_version": 1, "operation_id": "api-pair-assign", "arguments": args}
	code, data := adminRequest(t, f.H, ownerToken, http.MethodPost, pairAssignAPI+"/prepare", prepare)
	require.Equal(t, http.StatusForbidden, code, string(data))
	code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI+"/prepare", prepare)
	require.Equal(t, http.StatusOK, code, string(data))
	var prepared struct {
		Status        string `json:"status"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(data, &prepared))
	require.Equal(t, "prepared", prepared.Status)
	status := map[string]any{"protocol_version": 1, "operation_id": "api-pair-assign"}
	code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI+"/status", status)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	write := map[string]any{
		"protocol_version": 1,
		"operation_id":     "api-pair-assign",
		"idempotency_key":  "api-pair-assign",
		"arguments":        args,
	}
	code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusBadRequest, code, string(data))
	write["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": prepared.Preconditions.ProposalID},
	}
	code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusOK, code, string(data))
	var executed struct {
		Result db.PairAssignmentResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &executed))
	require.NotNil(t, executed.Result.Leader.Pass)
	require.NotNil(t, executed.Result.Follower.Pass)
	changed := args
	changed.ExpectedPairVersion++
	write["arguments"] = changed
	code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.Contains(t, string(data), `"classification":"terminal_rejection"`)
	f.H.stop(t)
	f.H.start(t)
	// Loom Status sends only protocol_version and operation_id, without arguments.
	for _, suffix := range []string{"status", "reconcile"} {
		code, data = adminRequest(t, f.H, adminToken, http.MethodPost, pairAssignAPI+"/"+suffix, status)
		require.Equal(t, http.StatusOK, code, string(data))
		var replay struct {
			Result db.PairAssignmentResult `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &replay))
		require.Equal(t, executed.Result, replay.Result)
	}
	code, data = adminRequest(t, f.H, ownerToken, http.MethodPost, pairAssignAPI+"/status", status)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	require.NotContains(t, string(data), executed.Result.Leader.Pass.ID.String())
}

func TestPairToolsRespectCurrentEventAdministratorAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, global, args := acceptedPairForAssignment(t)
	authority, err := f.H.store.ChangeAuthority(
		ctx,
		global.EventScope,
		global.Subject,
		"grant-pair-admin",
		db.AuthorityChange{
			EventID: f.EventID, UserID: f.Other.UserID, Role: db.AuthorityAdministrator,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	operator := pairUserToken(f, f.Other)
	code, data := adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"id":"assign"`)
	read := map[string]any{"protocol_version": 1, "operation_id": "pair-events", "arguments": map[string]any{}}
	code, data = adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/events", read)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), f.EventID.String())
	read["operation_id"] = "pair-candidates"
	read["arguments"] = map[string]any{"event_id": f.EventID}
	code, data = adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/candidates", read)
	require.Equal(t, http.StatusOK, code, string(data))
	var candidates struct {
		Result struct {
			Candidates []db.PairAssignmentCandidate `json:"candidates"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &candidates))
	require.Len(t, candidates.Result.Candidates, 1)
	require.Equal(t, args, candidates.Result.Candidates[0].Arguments)
	args = candidates.Result.Candidates[0].Arguments
	require.NotContains(t, string(data), "price_minor")
	owner := pairUserToken(f, f.A)
	code, data = adminRequest(t, f.H, owner, http.MethodPost, "/internal/agent/pairs/candidates", read)
	require.Equal(t, http.StatusForbidden, code, string(data))
	prepare := map[string]any{"protocol_version": 1, "operation_id": "event-admin-pair", "arguments": args}
	code, data = adminRequest(t, f.H, operator, http.MethodPost, pairAssignAPI+"/prepare", prepare)
	require.Equal(t, http.StatusOK, code, string(data))
	var prepared struct {
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(data, &prepared))
	_, err = f.H.store.ChangeAuthority(ctx, global.EventScope, global.Subject, "revoke-pair-admin", db.AuthorityChange{
		EventID: f.EventID, UserID: f.Other.UserID, Role: db.AuthorityNone, ExpectedVersion: authority.Version,
	}, uuid.Nil)
	require.NoError(t, err)
	code, data = adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), `"id":"assign"`)
	require.Contains(t, string(data), `"id":"my"`)
	require.Contains(t, string(data), `"id":"dissolve"`)
	code, data = adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/candidates", read)
	require.Equal(t, http.StatusForbidden, code, string(data))
	write := map[string]any{
		"protocol_version": 1,
		"operation_id":     "event-admin-pair",
		"idempotency_key":  "event-admin-pair",
		"arguments":        args,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": prepared.Preconditions.ProposalID},
		},
	}
	code, data = adminRequest(t, f.H, operator, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusForbidden, code, string(data))
}
