package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestOrdersSelectionAgentWireRecoveryAndAuthorization(t *testing.T) {
	t.Parallel()
	f, _, a := selectionFixture(t)
	const base = "/internal/agent/orders/"
	args := map[string]any{"event_id": a.EventID, "expected_event_version": a.ExpectedEventVersion,
		"expected_catalog_version": a.ExpectedCatalogVersion, "expected_draft_version": 0, "selections": a.Selections}
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     a.OperationID,
		"idempotency_key":  a.OperationID,
		"arguments":        args,
		"confirmation":     map[string]any{"mode": "none"},
	}
	read := func(token, path string, payload any) (string, *db.OrdersSelection) {
		t.Helper()
		code, raw := adminRequest(t, f.H, token, http.MethodPost, base+path, payload)
		require.Equal(t, http.StatusOK, code, string(raw))
		var receipt struct {
			Status string              `json:"status"`
			Result *db.OrdersSelection `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &receipt))
		return receipt.Status, receipt.Result
	}
	state, first := read(f.Owner, "save_selection", body)
	require.Equal(t, "succeeded", state)
	require.NotNil(t, first)
	require.Equal(t, int64(1), first.Version)
	state, result := read(
		f.Owner,
		"save_selection/status",
		map[string]any{"protocol_version": 1, "operation_id": a.OperationID},
	)
	require.Equal(t, "succeeded", state)
	require.Equal(t, first, result, "Exact Loom recovery envelope has no arguments")
	state, result = read(
		f.Owner,
		"selection_operation",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "lookup",
			"arguments":        map[string]any{"operation_id": a.OperationID},
		},
	)
	require.Equal(t, "succeeded", state)
	require.Equal(t, first, result)
	state, result = read(
		f.Ambassador,
		"save_selection/status",
		map[string]any{"protocol_version": 1, "operation_id": a.OperationID},
	)
	require.Equal(t, "not_found", state)
	require.Nil(t, result)
	state, result = read(
		f.Owner,
		"selection",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "read",
			"arguments":        map[string]any{"event_id": a.EventID},
		},
	)
	require.Equal(t, "succeeded", state)
	require.Equal(t, first, result)
	args["expected_draft_version"] = 1
	state, result = read(f.Owner, "save_selection", body)
	require.Equal(t, "rejected", state)
	require.Nil(t, result, "Operation ID cannot be reused with changed arguments")
	body["operation_id"], body["idempotency_key"] = uuid.New(), uuid.New()
	code, _ := adminRequest(t, f.H, f.Owner, http.MethodPost, base+"save_selection", body)
	require.Equal(t, http.StatusBadRequest, code)
	body["idempotency_key"] = body["operation_id"]
	args["owner_id"] = f.AmbassadorID
	code, _ = adminRequest(t, f.H, f.Owner, http.MethodPost, base+"save_selection", body)
	require.Equal(t, http.StatusUnprocessableEntity, code, "Caller cannot choose another owner")
	delete(args, "owner_id")
	args["expected_draft_version"] = 1
	state, result = read(f.Ambassador, "save_selection", body)
	require.Equal(t, "rejected", state)
	require.Nil(t, result, "Admin or ambassador role alone cannot borrow an assigned pass")
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodGet, "/internal/agent/manifest", nil)
	require.Equal(t, http.StatusOK, code)
	var manifest struct {
		Revision string `json:"revision"`
		Modules  []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID          string `json:"id"`
				Effect      string `json:"effect"`
				Idempotency bool   `json:"idempotency"`
				StatusPath  string `json:"status_path"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.NotEmpty(t, manifest.Revision, "Discovery must publish a revision for cache invalidation")
	var found bool
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_orders" {
			continue
		}
		for _, method := range module.Methods {
			if method.ID == "save_selection" {
				found = true
				require.Equal(t, "write", method.Effect)
				require.True(t, method.Idempotency)
				require.Equal(t, base+"save_selection/status", method.StatusPath)
			}
		}
	}
	require.True(t, found)
}
