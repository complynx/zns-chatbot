package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func paymentWorkspaceAPIFixture(t *testing.T) (paymentFixture, payments.ReviewChange) {
	t.Helper()
	f := setupPayment(t, newPeers(t))
	file := paymentFile(t, f.H, f.OwnerID, 19101, "workspace-api-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, "workspace-api-submit", file, 4000, payments.Initial)
	return f, payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
}

func workspaceAPIWrite(operation string, args any) map[string]any {
	return map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"idempotency_key":  operation,
		"arguments":        args,
		"confirmation":     map[string]any{"mode": "none"},
	}
}

func workspaceAPISaved(t *testing.T, f paymentFixture, review payments.ReviewChange) db.PaymentReviewWorkspace {
	t.Helper()
	code, raw := adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		paymentAPI+"save_review",
		workspaceAPIWrite("save-workspace", db.PaymentReviewDraftChange{Review: review}),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var result struct {
		Status      string                    `json:"status"`
		OperationID string                    `json:"operation_id"`
		Result      db.PaymentReviewWorkspace `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "save-workspace", result.OperationID)
	return result.Result
}

func workspaceAPIPrepared(
	t *testing.T,
	f paymentFixture,
	operation string,
	args db.PaymentReviewCommitChange,
) (map[string]any, uuid.UUID) {
	t.Helper()
	code, raw := paymentRequest(t, f, f.Ambassador, "review_commit/prepare", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var result struct {
		Status        string                       `json:"status"`
		OperationID   string                       `json:"operation_id"`
		Arguments     db.PaymentReviewCommitChange `json:"arguments"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "prepared", result.Status)
	require.Equal(t, operation, result.OperationID)
	require.Equal(t, args, result.Arguments)
	require.Contains(t, result.Description, "30.87")
	require.Contains(t, result.Description, f.Pass.Currency)
	require.Contains(t, result.Description, "Europe/Minsk")
	body := workspaceAPIWrite(operation, args)
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": result.Preconditions.ProposalID},
	}
	return body, result.Preconditions.ProposalID
}

func workspaceAPIStatus(t *testing.T, f paymentFixture, token, method, operation string) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		paymentAPI+method+"/status",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
}

func TestPaymentReviewWorkspaceAPIExactCommitAndRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, review := paymentWorkspaceAPIFixture(t)
	code, raw := paymentRequest(
		t,
		f,
		f.Ambassador,
		"review_workspace",
		"read-empty",
		map[string]any{"event_id": review.EventID, "evidence_id": review.EvidenceID},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"workspace":null`)
	w := workspaceAPISaved(t, f, review)
	balance, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), review.EventID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
	code, raw = paymentRequest(
		t,
		f,
		f.Ambassador,
		"review_workspace",
		"read-current",
		map[string]any{"event_id": review.EventID, "evidence_id": review.EvidenceID},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), w.ID.String())
	args := workspaceCommit(w)
	body, proposal := workspaceAPIPrepared(t, f, "loom-commit", args)
	require.NotEqual(t, w.Preparation.ID, proposal)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "loom-commit")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"pending"`)
	code, raw = paymentRequest(
		t,
		f,
		f.Ambassador,
		"review_operation",
		"inspect-call",
		map[string]any{"operation_id": "loom-commit"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"operation_id":"inspect-call"`)
	require.Contains(t, string(raw), proposal.String())
	require.Contains(t, string(raw), `"completed":false`)
	code, raw = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		paymentAPI+"review_commit",
		workspaceAPIWrite("loom-commit", args),
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	modified := args
	modified.Review.AmountMinor++
	body["arguments"] = modified
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review_commit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	body["arguments"] = args
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review_commit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var first struct {
		Status      string                          `json:"status"`
		OperationID string                          `json:"operation_id"`
		Result      db.PaymentWorkspaceReviewResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &first))
	require.Equal(t, "succeeded", first.Status)
	require.Equal(t, "loom-commit", first.OperationID)
	require.Equal(t, w.ID.String(), first.Result.CanonicalOperationID)
	require.EqualValues(t, 3087, first.Result.Review.Balance.ConfirmedMinor)
	code, replay := workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "loom-commit")
	require.Equal(t, http.StatusOK, code, string(replay))
	var recovered struct {
		Result db.PaymentWorkspaceReviewResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(replay, &recovered))
	require.Equal(t, first.Result, recovered.Result)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "save_review", "save-workspace")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"prepared"`)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "missing")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	code, raw = paymentRequest(
		t,
		f,
		f.Ambassador,
		"review_operation",
		"inspect-missing",
		map[string]any{"operation_id": "missing"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"receipt":null`)
}

func TestPaymentReviewWorkspaceAPINativeStoreCompletionReconcilesReadOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, review := paymentWorkspaceAPIFixture(t)
	w := workspaceAPISaved(t, f, review)
	args := workspaceCommit(w)
	body, _ := workspaceAPIPrepared(t, f, "waiting-loom-call", args)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	native, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "native-store-call", args)
	require.NoError(t, err)
	result, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "native-store-call", args, native.ID)
	require.NoError(t, err)
	code, raw := workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "waiting-loom-call")
	require.Equal(t, http.StatusOK, code, string(raw))
	var status struct {
		Status      string                          `json:"status"`
		OperationID string                          `json:"operation_id"`
		Result      db.PaymentWorkspaceReviewResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &status))
	require.Equal(t, "succeeded", status.Status)
	require.Equal(t, "waiting-loom-call", status.OperationID)
	require.Equal(t, result, status.Result)
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review_commit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &status))
	require.Equal(t, result, status.Result)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1`, review.EvidenceID).
			Scan(&count),
	)
	require.Equal(t, 1, count)
}

func TestPaymentReviewWorkspaceAPICancelExpiryAndFreshAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, review := paymentWorkspaceAPIFixture(t)
	w := workspaceAPISaved(t, f, review)
	args := workspaceCommit(w)
	body, _ := workspaceAPIPrepared(t, f, "cancelled-call", args)
	code, raw := adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		paymentAPI+"cancel_review",
		workspaceAPIWrite("cancel-call", db.PaymentReviewWorkspaceChange{WorkspaceID: w.ID, Revision: w.Revision}),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"succeeded"`)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "cancel_review", "cancel-call")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"cancelled"`)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "cancelled-call")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"cancelled"`)
	require.Contains(t, string(raw), `"code":"cancelled"`)
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"review_commit", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	code, raw = adminRequest(
		t,
		f.H,
		f.Ambassador,
		http.MethodPost,
		paymentAPI+"save_review",
		workspaceAPIWrite("replacement", db.PaymentReviewDraftChange{Review: review, ExpectedRevision: 1}),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var replacement struct {
		Result db.PaymentReviewWorkspace `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &replacement))
	args = workspaceCommit(replacement.Result)
	_, _ = workspaceAPIPrepared(t, f, "expired-call", args)
	_, err := f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_payment_preparations SET expires_at=clock_timestamp()-interval '1 hour' WHERE "+
			"proposal_id=$1", replacement.Result.Preparation.ID)
	require.NoError(t, err)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "review_commit", "expired-call")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"cancelled"`)
	require.Contains(t, string(raw), `"code":"expired"`)
	code, raw = paymentRequest(
		t,
		f,
		f.Owner,
		"review_workspace",
		"owner-forbidden",
		map[string]any{"event_id": review.EventID, "evidence_id": review.EvidenceID},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = paymentRequest(
		t,
		f,
		f.Owner,
		"review_operation",
		"foreign-call",
		map[string]any{"operation_id": "save-workspace"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"receipt":null`)
	admin := paymentScope(t, f.H, f.AdminID, true)
	_, err = f.H.store.ChangeAuthority(
		ctx,
		admin.EventScope,
		admin.Subject,
		"revoke-api-review",
		db.AuthorityChange{EventID: review.EventID, UserID: f.AmbassadorID, Role: db.AuthorityNone, ExpectedVersion: 1},
		uuid.Nil,
	)
	require.NoError(t, err)
	code, raw = workspaceAPIStatus(t, f, f.Ambassador, "save_review", "save-workspace")
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = paymentRequest(
		t,
		f,
		f.Ambassador,
		"review_operation",
		"revoked-read",
		map[string]any{"operation_id": "expired-call"},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	balance, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), review.EventID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
}

func TestPaymentReviewWorkspaceAPIDiscoveryRequiresExactMoneyApproval(t *testing.T) {
	t.Parallel()
	f, _ := paymentWorkspaceAPIFixture(t)
	code, raw := adminRequest(t, f.H, f.Ambassador, http.MethodPost, paymentAPI+"discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var discovery struct {
		Methods []struct {
			ID           string `json:"id"`
			Effect       string `json:"effect"`
			Idempotency  bool   `json:"idempotency"`
			StatusPath   string `json:"status_path"`
			Confirmation struct {
				Mode        string `json:"mode"`
				PreparePath string `json:"prepare_path"`
			} `json:"confirmation"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"input_schema"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	seen := map[string]bool{}
	for _, m := range discovery.Methods {
		switch m.ID {
		case "save_review", "cancel_review", "review_commit":
			seen[m.ID] = true
			require.Equal(t, "write", m.Effect)
			require.True(t, m.Idempotency)
			require.Equal(t, paymentAPI+m.ID+"/status", m.StatusPath)
			if m.ID != "review_commit" {
				require.Equal(t, "none", m.Confirmation.Mode)
				continue
			}
			require.Equal(t, "exact_parameters", m.Confirmation.Mode)
			require.Equal(t, paymentAPI+"review_commit/prepare", m.Confirmation.PreparePath)
			require.ElementsMatch(t, []string{"workspace_id", "revision", "review"}, m.InputSchema.Required)
			var review struct {
				Required             []string                   `json:"required"`
				Properties           map[string]json.RawMessage `json:"properties"`
				AdditionalProperties bool                       `json:"additionalProperties"`
			}
			require.NoError(t, json.Unmarshal(m.InputSchema.Properties["review"], &review))
			require.False(t, review.AdditionalProperties)
			require.ElementsMatch(
				t,
				[]string{"event_id", "evidence_id", "expected_version", "decision", "amount_minor", "reason"},
				review.Required,
			)
			for _, field := range review.Required {
				require.Contains(t, review.Properties, field)
			}
		case "review_workspace", "review_operation":
			seen[m.ID] = true
			require.Equal(t, "read", m.Effect)
			require.False(t, m.Idempotency)
		}
	}
	require.Len(t, seen, 5)
}
