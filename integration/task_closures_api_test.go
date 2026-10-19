package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const taskClosureAPI = "/internal/agent/administration/close_task"

func TestAgentTaskClosureLostResponseReconcilesWithoutRepeatingClosure(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	args.Reason = strings.Repeat("Я", 512)
	operator := taskAdminToken(t, h, args.TaskID)
	body := prepareTaskClosureBody(t, h, operator, "lost-closure-response", args)
	before := taskClosureDiagnostics(t, h, scope, args.TaskID)
	committed := make(chan *httptest.ResponseRecorder, 1)
	transportErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := httptest.NewRecorder()
		h.endpoint.InternalHandler().ServeHTTP(response, r)
		committed <- response
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			err = conn.Close()
		}
		transportErrors <- err
	}))
	t.Cleanup(server.Close)
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		server.URL+taskClosureAPI, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+operator)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Error(t, err)
	require.NoError(t, <-transportErrors)
	first := <-committed
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"status":"succeeded"`)
	var original map[string]any
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &original))
	delete(original, "$schema") // Huma's schema URL follows each test server's address.
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, recovered := adminRequest(t, h, operator, http.MethodPost, taskClosureAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "lost-closure-response"})
		require.Equal(t, http.StatusOK, code, string(recovered))
		var receipt map[string]any
		require.NoError(t, json.Unmarshal(recovered, &receipt))
		delete(receipt, "$schema")
		sameTaskClosureJSON(t, original, receipt)
	}
	after := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Len(t, after.Resolutions, 1)
	require.Equal(t, before.Task.Version+1, after.Task.Version)
	require.Equal(t, before.Deliveries, after.Deliveries)
}

func prepareTaskClosureBody(
	t *testing.T, h *harness, token, op string, args db.TaskClosureArguments,
) map[string]any {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
	code, raw := adminRequest(t, h, token, http.MethodPost, taskClosureAPI+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Status        string                  `json:"status"`
		Arguments     db.TaskClosureArguments `json:"arguments"`
		Description   string                  `json:"description"`
		Preconditions map[string]any          `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "prepared", prep.Status)
	require.Equal(t, args, prep.Arguments)
	require.Contains(t, prep.Description, "does not establish the external outcome")
	require.NotContains(t, prep.Description, "private-message-canary")
	body["arguments"] = prep.Arguments
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}

func TestAgentTaskClosureRequiresRoleAndExactApprovalAndRecoversReceipt(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	basic := h.peers.token(scope.Subject)
	initial := map[string]any{"protocol_version": 1, "operation_id": "agent-closure", "arguments": args}
	code, raw := adminRequest(t, h, basic, http.MethodPost, taskClosureAPI+"/prepare", initial)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	operator := taskAdminToken(t, h, args.TaskID)
	body := prepareTaskClosureBody(t, h, operator, "agent-closure", args)
	confirmation := body["confirmation"]
	delete(body, "confirmation")
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = map[string]any{"mode": "none"}
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = confirmation
	body["idempotency_key"] = "another-operation"
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = "agent-closure"
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": uuid.New()},
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.Contains(t, string(raw), `"classification":"terminal_rejection"`)
	body["confirmation"] = confirmation
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var first struct {
		Status string               `json:"status"`
		Result db.TaskClosureResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &first))
	require.Equal(t, "succeeded", first.Status)
	require.Equal(t, args.TaskID, first.Result.TaskID)
	_, err := h.store.Pool().Exec(context.Background(),
		"UPDATE shuttle_task_closure_proposals SET expires_at=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "agent-closure"})
		require.Equal(t, http.StatusOK, code, string(raw))
		var replay struct {
			Status string               `json:"status"`
			Result db.TaskClosureResult `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &replay))
		sameTaskClosureJSON(t, first, replay)
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "private-message-canary")
	diagnostics := taskClosureDiagnostics(t, h, scope, args.TaskID)
	require.Len(t, diagnostics.Resolutions, 1)
	require.Equal(t, "closed", diagnostics.Task.State)
	_, err = h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "agent-closure"})
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestAgentTaskClosureDiscoveryAndStatusExcludeOperatorReceipts(t *testing.T) {
	t.Parallel()
	h, scope, args := taskClosureFixture(t)
	operator := taskAdminToken(t, h, args.TaskID)
	code, raw := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/discover",
		map[string]any{},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var discovery struct {
		Methods []struct {
			ID           string         `json:"id"`
			Effect       string         `json:"effect"`
			Idempotency  bool           `json:"idempotency"`
			StatusPath   string         `json:"status_path"`
			Confirmation map[string]any `json:"confirmation"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	found := false
	for _, method := range discovery.Methods {
		if method.ID == "close_task" {
			found = true
			require.Equal(t, "write", method.Effect)
			require.True(t, method.Idempotency)
			require.Equal(t, taskClosureAPI+"/status", method.StatusPath)
			require.Equal(t, "exact_parameters", method.Confirmation["mode"])
			require.Equal(t, taskClosureAPI+"/prepare", method.Confirmation["prepare_path"])
		}
	}
	require.True(t, found)
	_, err := h.store.CloseUncertainTask(context.Background(), db.CloseTaskRequest{
		TaskID: args.TaskID, BotID: scope.BotID, Organization: scope.Organization, Actor: scope.Subject,
		OperationID: "operator-only", Reason: args.Reason, ExpectedVersion: args.ExpectedVersion,
	})
	require.NoError(t, err)
	code, raw = adminRequest(t, h, operator, http.MethodPost, taskClosureAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "operator-only"})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
}
