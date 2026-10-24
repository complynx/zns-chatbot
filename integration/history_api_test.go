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

func historyWrite(t *testing.T, h *harness, token string, r db.HistoryApplyRequest) (int, []byte) {
	t.Helper()
	return adminRequest(t, h, token, http.MethodPost, "/internal/agent/administration/apply_history", map[string]any{
		"protocol_version": 1,
		"operation_id":     r.OperationID,
		"idempotency_key":  r.OperationID,
		"arguments":        r.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": r.ProposalID},
		},
	})
}

func TestHistoryAgentAPIRequiresExactApprovalAndReadsOnlyCurrentOwner(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, admin := registrationUser(t, h, 42, true)
	_, ordinary := registrationUser(t, h, 43, false)
	scope := historyScope(t, h, id)
	raw, digest := historyBundle(
		t,
		"api-history",
		map[string][]map[string]any{
			"users":    {historyUser(42)},
			"messages": {historyMessage("m1", "Private history text", 42)},
		},
	)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := db.HistoryApplyRequest{OperationID: uuid.New(), Arguments: a}
	code, body := agentAdminRead(t, h, ordinary, "apply_history/prepare", a)
	require.Equal(t, http.StatusForbidden, code, string(body))
	code, body = adminRequest(
		t,
		h,
		admin,
		http.MethodPost,
		"/internal/agent/administration/apply_history/prepare",
		map[string]any{"protocol_version": 1, "operation_id": r.OperationID, "arguments": a},
	)
	require.Equal(t, http.StatusOK, code, string(body))
	var prepared struct {
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(body, &prepared))
	r.ProposalID = prepared.Preconditions.ProposalID
	code, body = historyWrite(t, h, ordinary, r)
	require.Equal(t, http.StatusForbidden, code, string(body))
	wrong := r
	wrong.ProposalID = uuid.New()
	code, body = historyWrite(t, h, admin, wrong)
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, string(body), `"status":"rejected"`)
	require.Contains(t, string(body), `"classification":"terminal_rejection"`)
	code, body = historyWrite(t, h, admin, r)
	require.Equal(t, http.StatusOK, code, string(body))
	var receipt struct {
		Status string                 `json:"status"`
		Result db.HistoryApplyReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &receipt))
	require.Equal(t, "succeeded", receipt.Status)
	code, body = adminRequest(
		t,
		h,
		admin,
		http.MethodPost,
		"/internal/agent/administration/apply_history/status",
		map[string]any{"protocol_version": 1, "operation_id": r.OperationID},
	)
	require.Equal(t, http.StatusOK, code, string(body))
	var status struct {
		Result db.HistoryApplyReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &status))
	require.Equal(t, receipt.Result, status.Result)
	read := func(token, method string, args any) (int, []byte) {
		return adminRequest(
			t,
			h,
			token,
			http.MethodPost,
			"/internal/agent/history/"+method,
			map[string]any{"protocol_version": 1, "operation_id": "history-read", "arguments": args},
		)
	}
	code, body = read(admin, "page", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(body))
	var page struct {
		Result db.HistoryPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Result.Items, 2)
	var message uuid.UUID
	for _, item := range page.Result.Items {
		if item.SourceRole == "messages" {
			message = item.ID
		}
	}
	code, body = read(admin, "text", map[string]any{"id": message})
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, string(body), "Private history text")
	code, body = read(ordinary, "page", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, string(body), `"items":[]`)
	code, body = read(ordinary, "text", map[string]any{"id": message})
	require.Equal(t, http.StatusNotFound, code, string(body))
	code, body = read(ordinary, "page", map[string]any{"owner_telegram_id": 42})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(body))
	_, err := h.store.Pool().Exec(context.Background(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", id)
	require.NoError(t, err)
	code, _ = read(admin, "text", map[string]any{"id": message})
	require.Equal(t, http.StatusForbidden, code)
}
