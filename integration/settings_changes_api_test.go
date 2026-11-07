package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const settingsChangeAPI = "/internal/agent/administration/update_settings"

func settingsAdminToken(h *harness, scope db.AdminScope) string {
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	h.peers.adminRole = true
	return h.peers.token(scope.Subject)
}

func prepareSettingsBody(t *testing.T, h *harness, token, op string, args db.SettingsChangeArguments) map[string]any {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
	code, raw := adminRequest(t, h, token, http.MethodPost, settingsChangeAPI+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Status        string                     `json:"status"`
		Arguments     db.SettingsChangeArguments `json:"arguments"`
		Preconditions map[string]any             `json:"preconditions"`
		Description   string                     `json:"description"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "prepared", prep.Status)
	require.Equal(t, args, prep.Arguments)
	require.Contains(t, prep.Description, "application-wide")
	body["arguments"] = prep.Arguments
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}

func readSettingsResult(t *testing.T, raw []byte) db.VersionedSettings {
	t.Helper()
	var result struct {
		Status string               `json:"status"`
		Result db.VersionedSettings `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}

func TestAgentSettingsChangeRequiresRoleExactApprovalAndReplaysAfterRestart(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	h.start(t)
	h.stop(t)
	basic := h.peers.token(scope.Subject)
	code, raw := adminRequest(t, h, basic, http.MethodPost, settingsChangeAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": "settings-api", "arguments": args})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	operator := settingsAdminToken(h, scope)
	body := prepareSettingsBody(t, h, operator, "settings-api", args)
	confirmation := body["confirmation"]
	delete(body, "confirmation")
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = map[string]any{"mode": "none"}
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = confirmation
	body["idempotency_key"] = "wrong-operation"
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = "settings-api"
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": uuid.New()},
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.Zero(t, settingsAuditCount(t, h))
	body["confirmation"] = confirmation
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	first := readSettingsResult(t, raw)
	require.Equal(t, args.Document, first.Document)
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(
			t,
			h,
			operator,
			http.MethodPost,
			settingsChangeAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "settings-api"},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, first, readSettingsResult(t, raw))
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, first, readSettingsResult(t, raw))
	require.EqualValues(t, 1, settingsAuditCount(t, h))
	_, err := h.store.SetUserRevoked(t.Context(), scope.UserID, "operator")
	require.NoError(t, err)
	code, raw = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		settingsChangeAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "settings-api"},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestAgentSettingsPollingPreservesCacheAndOldReceiptCannotRestoreOldTTL(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	args.Document.AuthCacheTTLSeconds = 120
	body := prepareSettingsBody(t, h, operator, "cached-settings", args)
	code, raw := adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	first := readSettingsResult(t, raw)
	statusBody := map[string]any{"protocol_version": 1, "operation_id": "cached-settings"}
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	for range 3 {
		code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI+"/status", statusBody)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, first, readSettingsResult(t, raw))
	}
	h.peers.mu.Lock()
	h.peers.iamDown = false
	h.peers.mu.Unlock()
	newer := first
	newer.Document.AuthCacheTTLSeconds = 0
	code, raw = adminRequest(t, h, operator, http.MethodPut, "/internal/admin/settings", newer)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, first, readSettingsResult(t, raw))
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	require.EqualValues(t, 2, settingsAuditCount(t, h))
}

func TestAgentSettingsCommittedResultSurvivesFailedCurrentReadAndCacheFailsClosed(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	initial := db.VersionedSettings{Version: args.ExpectedVersion, Document: args.Document}
	initial.Document.AuthCacheTTLSeconds = 120
	code, raw := adminRequest(t, h, operator, http.MethodPut, "/internal/admin/settings", initial)
	require.Equal(t, http.StatusOK, code, string(raw))
	args.ExpectedVersion++
	args.Document.AuthCacheTTLSeconds = 60
	body := prepareSettingsBody(t, h, operator, "read-failure-settings", args)
	_, err := h.store.Pool().
		Exec(t.Context(), `CREATE FUNCTION hide_current_settings() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN DELETE FROM shuttle_settings; RETURN NEW; END $$;
CREATE TRIGGER hide_current_settings AFTER UPDATE ON shuttle_settings_change_proposals
FOR EACH ROW EXECUTE FUNCTION hide_current_settings()`)
	require.NoError(t, err)
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	receipt, err := h.store.SettingsChangeReceipt(t.Context(), scope, "read-failure-settings")
	require.NoError(t, err)
	require.Equal(t, args.Document, receipt.Document)
	require.EqualValues(t, 2, settingsAuditCount(t, h))
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	h.peers.mu.Lock()
	h.peers.iamDown = false
	h.peers.mu.Unlock()
	document, err := json.Marshal(receipt.Document)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_settings(id,version,document) VALUES(true,$1,$2)", receipt.Version, document)
	require.NoError(t, err)
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI+"/reconcile",
		map[string]any{"protocol_version": 1, "operation_id": "read-failure-settings"})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, *receipt, readSettingsResult(t, raw))
	code, raw = adminRequest(t, h, operator, http.MethodPost, settingsChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, *receipt, readSettingsResult(t, raw))
	require.EqualValues(t, 2, settingsAuditCount(t, h))
}
