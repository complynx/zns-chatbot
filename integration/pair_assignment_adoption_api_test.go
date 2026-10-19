package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

type pairAdoptionArguments struct {
	db.PairAssignment

	ResumeProposalID uuid.UUID `json:"resume_proposal_id"`
}

type pairAdoptionPreparation struct {
	Status        string            `json:"status"`
	OperationID   string            `json:"operation_id"`
	Arguments     db.PairAssignment `json:"arguments"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
	Description string    `json:"description"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func pairAdoptionRead(t *testing.T, f pairFixture, token, method string, arguments any) (int, []byte) {
	t.Helper()
	return adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/"+method,
		map[string]any{"protocol_version": 1, "operation_id": "inspect-pair", "arguments": arguments})
}

func pairAdoptionPrepare(
	t *testing.T,
	f pairFixture,
	token, operation string,
	args db.PairAssignment,
	proposal uuid.UUID,
) (int, []byte) {
	t.Helper()
	return adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": operation,
			"arguments": pairAdoptionArguments{PairAssignment: args, ResumeProposalID: proposal}})
}

func pairAdoptionWrite(operation string, args any, proposal uuid.UUID) map[string]any {
	return map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": proposal},
		},
	}
}

func TestPairAssignmentAdoptionAPIPreservesTermsAndCanonicalReceipt(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	token := pairUserToken(f, admin)
	original, err := f.H.store.PreparePairAssignment(t.Context(), admin, "native-pair-original", args)
	require.NoError(t, err)
	for _, operation := range []string{"loom-pair-envelope", "second-pair-envelope"} {
		code, raw := pairAdoptionPrepare(t, f, token, operation, args, original.ID)
		require.Equal(t, http.StatusOK, code, string(raw))
		var adopted pairAdoptionPreparation
		require.NoError(t, json.Unmarshal(raw, &adopted))
		require.Equal(t, "prepared", adopted.Status)
		require.Equal(t, operation, adopted.OperationID)
		require.Equal(t, original.ID, adopted.Preconditions.ProposalID)
		require.Equal(t, original.Arguments, adopted.Arguments)
		require.Equal(t, original.Description, adopted.Description)
		require.True(t, original.ExpiresAt.Equal(adopted.ExpiresAt))
		require.NotContains(t, string(raw), "resume_proposal_id")
	}
	code, raw := pairAdoptionRead(t, f, token, "assignment_proposal", map[string]any{"proposal_id": original.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var inspected struct {
		Result db.PairAssignmentProposalView `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &inspected))
	require.Equal(t, "native-pair-original", inspected.Result.OriginalOperationID)
	require.Equal(t, original, inspected.Result.Preparation)
	require.Equal(t, "pending", string(inspected.Result.State))
	require.Nil(t, inspected.Result.Result)
	// Adoption records context only; confirmation remains a separate exact write.
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
	f.H.stop(t)
	f.H.start(t)
	write := pairAdoptionWrite("loom-pair-envelope", pairAdoptionArguments{args, original.ID}, original.ID)
	code, raw = adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusOK, code, string(raw))
	var committed struct {
		Status string                  `json:"status"`
		Result db.PairAssignmentResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &committed))
	require.Equal(t, "succeeded", committed.Status)
	require.NotNil(t, committed.Result.Leader.Pass)
	require.NotNil(t, committed.Result.Follower.Pass)
	for _, operation := range []string{"native-pair-original", "loom-pair-envelope", "second-pair-envelope"} {
		for _, suffix := range []string{"status", "reconcile"} {
			code, raw = adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI+"/"+suffix,
				map[string]any{"protocol_version": 1, "operation_id": operation})
			require.Equal(t, http.StatusOK, code, string(raw))
			var receipt struct {
				OperationID string                  `json:"operation_id"`
				Result      db.PairAssignmentResult `json:"result"`
			}
			require.NoError(t, json.Unmarshal(raw, &receipt))
			require.Equal(t, operation, receipt.OperationID)
			require.Equal(t, committed.Result, receipt.Result)
		}
	}
	code, raw = pairAdoptionRead(t, f, token, "assignment_proposal", map[string]any{"proposal_id": original.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &inspected))
	require.Equal(t, "completed", string(inspected.Result.State))
	require.Equal(t, committed.Result, *inspected.Result.Result)
	code, raw = pairAdoptionPrepare(t, f, token, "after-completion", args, original.ID)
	require.Equal(t, http.StatusConflict, code, string(raw))
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 2, passes)
}

func TestPairAssignmentAdoptionAPIRejectsForeignChangedExpiredAndUnconfirmed(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	token := pairUserToken(f, admin)
	original, err := f.H.store.PreparePairAssignment(t.Context(), admin, "pair-original", args)
	require.NoError(t, err)
	foreign := f.Other
	foreign.GlobalAdmin = true
	foreignToken := pairUserToken(f, foreign)
	code, raw := pairAdoptionPrepare(t, f, foreignToken, "foreign-adopt", args, original.ID)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = pairAdoptionRead(t, f, foreignToken, "assignment_proposal", map[string]any{"proposal_id": original.ID})
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = pairAdoptionPrepare(t, f, token, "missing-proposal", args, uuid.New())
	require.Equal(t, http.StatusNotFound, code, string(raw))
	changed := args
	changed.ExpectedLeaderVersion++
	code, raw = pairAdoptionPrepare(t, f, token, "changed-adopt", changed, original.ID)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw = pairAdoptionPrepare(t, f, token, "exact-adopt", args, original.ID)
	require.Equal(t, http.StatusOK, code, string(raw))
	write := pairAdoptionWrite("exact-adopt", pairAdoptionArguments{args, original.ID}, uuid.New())
	code, raw = adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	delete(write, "confirmation")
	code, raw = adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	// A matching resume field in a write cannot perform adoption implicitly.
	write = pairAdoptionWrite("unprepared-transport", pairAdoptionArguments{args, original.ID}, original.ID)
	code, raw = adminRequest(t, f.H, token, http.MethodPost, pairAssignAPI, write)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	_, err = f.H.store.Pool().Exec(t.Context(),
		"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", original.ID)
	require.NoError(t, err)
	code, raw = pairAdoptionPrepare(t, f, token, "expired-adopt", args, original.ID)
	require.Equal(t, http.StatusConflict, code, string(raw))
	code, raw = pairAdoptionRead(t, f, token, "assignment_proposal", map[string]any{"proposal_id": original.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"expired"`)
	code, raw = pairAdoptionRead(t, f, token, "assignment_proposals", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), original.ID.String())
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestPairAssignmentProposalAPIChecksCurrentEventAuthority(t *testing.T) {
	t.Parallel()
	f, global, args := acceptedPairForAssignment(t)
	authority, err := f.H.store.ChangeAuthority(t.Context(), global.EventScope, global.Subject, "grant-proposal-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.Other.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	operator := pairUserToken(f, f.Other)
	original, err := f.H.store.PreparePairAssignment(t.Context(), f.Other, "event-admin-original", args)
	require.NoError(t, err)
	code, raw := adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"id":"assignment_proposal"`)
	require.Contains(t, string(raw), `"id":"assignment_proposals"`)
	for _, method := range []string{"assignment_proposal", "assignment_proposals"} {
		arguments := map[string]any{}
		if method == "assignment_proposal" {
			arguments["proposal_id"] = original.ID
		}
		code, raw = pairAdoptionRead(t, f, operator, method, arguments)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), original.ID.String())
	}
	code, raw = pairAdoptionRead(t, f, operator, "assignment_proposals", map[string]any{"limit": 21})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
	code, raw = pairAdoptionPrepare(t, f, operator, "event-admin-envelope", args, original.ID)
	require.Equal(t, http.StatusOK, code, string(raw))
	_, err = f.H.store.ChangeAuthority(
		t.Context(),
		global.EventScope,
		global.Subject,
		"revoke-proposal-admin",
		db.AuthorityChange{
			EventID:         f.EventID,
			UserID:          f.Other.UserID,
			Role:            db.AuthorityNone,
			ExpectedVersion: authority.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	code, raw = pairAdoptionRead(t, f, operator, "assignment_proposal", map[string]any{"proposal_id": original.ID})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = pairAdoptionPrepare(t, f, operator, "revoked-adopt", args, original.ID)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = adminRequest(t, f.H, operator, http.MethodPost, pairAssignAPI,
		pairAdoptionWrite("event-admin-envelope", pairAdoptionArguments{args, original.ID}, original.ID))
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = pairAdoptionRead(t, f, operator, "assignment_proposals", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), original.ID.String())
	code, raw = adminRequest(t, f.H, operator, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), `"id":"assignment_proposal"`)
	require.NotContains(t, string(raw), `"id":"assignment_proposals"`)
}
