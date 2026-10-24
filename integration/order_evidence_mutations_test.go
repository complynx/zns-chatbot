package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func orderEvidenceWrite(
	t *testing.T,
	f paymentFixture,
	token, method string,
	op uuid.UUID,
	args any,
	proposal uuid.UUID,
) []byte {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "idempotency_key": op, "arguments": args}
	if proposal != uuid.Nil {
		body["confirmation"] = map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": proposal},
		}
	}
	code, raw := adminRequest(t, f.H, token, http.MethodPost, orderEvidenceAPI+method, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	return raw
}

func TestOrderEvidenceMutationSubmissionStatusRetainsOriginalClaim(t *testing.T) {
	t.Parallel()
	f, _, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61501, "mutation-owner-proof")
	op := uuid.New()
	var first orderWireResult[db.OrderPaymentEvidence]
	raw := orderEvidenceWrite(t, f, f.Owner, "submit", op, args, uuid.Nil)
	require.NoError(t, json.Unmarshal(raw, &first))
	require.Equal(t, "succeeded", first.Status)
	require.NotNil(t, first.Result)
	_, _, confirmed := reviewOrderEvidence(t, f, *first.Result, db.OrderEvidenceConfirmed)
	require.Equal(t, db.OrderEvidenceConfirmed, confirmed.State)
	code, status := adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		orderEvidenceAPI+"submit/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, http.StatusOK, code, string(status))
	var recovered orderWireResult[db.OrderPaymentEvidence]
	require.NoError(t, json.Unmarshal(status, &recovered))
	require.Equal(t, first, recovered)
	var retried orderWireResult[db.OrderPaymentEvidence]
	require.NoError(t, json.Unmarshal(orderEvidenceWrite(t, f, f.Owner, "submit", op, args, uuid.Nil), &retried))
	require.Equal(t, first, retried)
}

func TestOrderEvidenceMutationNativeCompletionResolvesAgentReadOnly(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	var saved orderWireResult[db.OrderReviewWorkspace]
	raw := orderEvidenceWrite(
		t,
		f,
		f.Ambassador,
		"save_review",
		uuid.New(),
		db.OrderReviewDraftChange{Review: review, Language: "ru"},
		uuid.Nil,
	)
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, "succeeded", saved.Status)
	require.NotNil(t, saved.Result)
	w := *saved.Result
	require.Contains(t, w.Preparation.Description, "Подтвердить оплату заказа?")
	require.Contains(t, w.Preparation.Description, review.Reason)
	args, op := orderReviewCommit(w), uuid.New()
	code, raw := adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		orderEvidenceAPI+"review_commit/prepare",
		map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Description   string `json:"description"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, w.Preparation.Description, prep.Description)
	require.NotEqual(t, uuid.Nil, prep.Preconditions.ProposalID)
	session := nativePaymentSession(t, f, scope, 11003)
	first, err := f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		uuid.New(),
		args,
		w.Preparation.ID,
	)
	require.NoError(t, err)
	readStatus := func() orderWireResult[db.OrderWorkspaceReviewResult] {
		t.Helper()
		statusCode, statusRaw := adminRequest(
			t,
			f.H,
			f.Ambassador,
			http.MethodPost,
			orderEvidenceAPI+"review_commit/status",
			map[string]any{"protocol_version": 1, "operation_id": op},
		)
		require.Equal(t, http.StatusOK, statusCode, string(statusRaw))
		var out orderWireResult[db.OrderWorkspaceReviewResult]
		require.NoError(t, json.Unmarshal(statusRaw, &out))
		return out
	}
	for range 2 {
		out := readStatus()
		require.Equal(t, "succeeded", out.Status)
		require.NotNil(t, out.Result)
		require.Equal(t, first, *out.Result)
	}
	var completed bool
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT result IS NOT NULL FROM shuttle_order_review_calls WHERE actor_id" +
"=$1 AND operation_id=$2", scope.UserID, op).
			Scan(&completed),
	)
	require.False(t, completed, "status must not create an outer write receipt")
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
}

func TestOrderEvidenceMutationCancelledIntentHasTerminalStatus(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review, Language: "en"},
	)
	require.NoError(t, err)
	require.Contains(t, w.Preparation.Description, "Confirm order payment?")
	op := uuid.New()
	_, err = f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, orderReviewCommit(w))
	require.NoError(t, err)
	raw := orderEvidenceWrite(
		t,
		f,
		f.Ambassador,
		"cancel_review",
		uuid.New(),
		db.OrderReviewWorkspaceChange{WorkspaceID: w.ID, Revision: w.Revision},
		uuid.Nil,
	)
	require.Contains(t, string(raw), `"status":"succeeded"`)
	code, raw := adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		orderEvidenceAPI+"review_commit/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"cancelled"`)
	require.NotContains(t, string(raw), `"status":"succeeded"`)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
}

func TestOrderEvidenceMutationNativeDraftAgentCommitReplaysOneDecision(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review, Language: "en"},
	)
	require.NoError(t, err)
	args, op := orderReviewCommit(w), uuid.New()
	code, raw := adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		orderEvidenceAPI+"review_commit/prepare",
		map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Description   string `json:"description"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, w.Preparation.Description, prep.Description)
	changed := args
	changed.Review.Reason = "Changed after consent"
	var denied orderWireResult[db.OrderWorkspaceReviewResult]
	require.NoError(
		t,
		json.Unmarshal(
			orderEvidenceWrite(t, f, f.Ambassador, "review_commit", op, changed, prep.Preconditions.ProposalID),
			&denied,
		),
	)
	require.Equal(t, "rejected", denied.Status)
	require.Nil(t, denied.Result)
	var first orderWireResult[db.OrderWorkspaceReviewResult]
	require.NoError(
		t,
		json.Unmarshal(
			orderEvidenceWrite(t, f, f.Ambassador, "review_commit", op, args, prep.Preconditions.ProposalID),
			&first,
		),
	)
	require.Equal(t, "succeeded", first.Status)
	require.NotNil(t, first.Result)
	require.Equal(t, w.ID, first.Result.CanonicalOperationID)
	require.Equal(t, db.OrderEvidenceConfirmed, first.Result.Evidence.State)
	var replay orderWireResult[db.OrderWorkspaceReviewResult]
	require.NoError(
		t,
		json.Unmarshal(
			orderEvidenceWrite(t, f, f.Ambassador, "review_commit", op, args, prep.Preconditions.ProposalID),
			&replay,
		),
	)
	require.Equal(t, first, replay)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
}
