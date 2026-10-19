package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func taskAdminToken(t *testing.T, h *harness, id uuid.UUID) string {
	t.Helper()
	task, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	user, err := h.store.GetUser(context.Background(), task.UserID)
	require.NoError(t, err)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	h.peers.adminRole = true
	return h.peers.token(user.Subject)
}

func adminRequest(t *testing.T, h *harness, token, method, path string, body any) (int, []byte) {
	t.Helper()
	server := httptest.NewServer(h.endpoint.InternalHandler())
	defer server.Close()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, result
}

func adminDiagnostics(t *testing.T, h *harness, token string, id uuid.UUID) db.TaskDiagnostics {
	t.Helper()
	code, data := adminRequest(t, h, token, http.MethodGet, "/internal/admin/tasks/"+id.String(), nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var result db.TaskDiagnostics
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

func unknownTask(t *testing.T, h *harness) uuid.UUID {
	t.Helper()
	id := h.submitText(t, 80, "private-message-canary")
	h.await(t, id, "delivery_unknown")
	require.Eventually(t, func() bool {
		var active bool
		err := h.store.Pool().
			QueryRow(context.Background(), "SELECT lease_until IS NOT NULL FROM shuttle_tasks WHERE id=$1", id).
			Scan(&active)
		return err == nil && !active
	}, 5*time.Second, 20*time.Millisecond)
	return id
}

func TestAdminTaskDiagnosticsRequireRoleScopeAndRedactContent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := unknownTask(t, h)
	task, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	user, err := h.store.GetUser(context.Background(), task.UserID)
	require.NoError(t, err)
	basic := p.token(user.Subject)
	code, _ := adminRequest(t, h, basic, http.MethodGet, "/internal/admin/tasks", nil)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = adminRequest(
		t,
		h,
		basic,
		http.MethodPost,
		"/internal/admin/tasks/"+id.String()+"/close",
		map[string]any{"operation_id": "denied", "expected_version": task.Version, "reason": "denied", "confirm": true},
	)
	require.Equal(t, http.StatusForbidden, code)
	operator := taskAdminToken(t, h, id)
	result := adminDiagnostics(t, h, operator, id)
	require.Equal(t, "delivery_unknown", result.Task.State)
	require.Len(t, result.Deliveries, 1)
	require.Len(t, result.QueueJobs, 1)
	code, data := adminRequest(t, h, operator, http.MethodGet, "/internal/admin/tasks/"+id.String(), nil)
	require.Equal(t, http.StatusOK, code)
	for _, private := range []string{"private-message-canary", "payload", "snapshot", "profile", "auth_session_id"} {
		require.NotContains(t, string(data), private)
	}
	foreign, _, err := h.store.Intake(
		context.Background(),
		db.IntakeRequest{
			BotID:          124,
			UpdateID:       81,
			TelegramUserID: 789,
			ChatID:         789,
			Payload:        json.RawMessage(`{"kind":"message"}`),
		},
		nil,
	)
	require.NoError(t, err)
	code, _ = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/tasks/"+foreign.ID.String(), nil)
	require.Equal(t, http.StatusNotFound, code)
	code, data = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/tasks?limit=1", nil)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, string(data), id.String())
	require.NotContains(t, string(data), foreign.ID.String())
	code, _ = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/tasks?limit=101", nil)
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

func TestAdminClosureIsConfirmedAuditedIdempotentAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := unknownTask(t, h)
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	path := "/internal/admin/tasks/" + id.String() + "/close"
	body := map[string]any{
		"operation_id":     "close-80",
		"expected_version": before.Task.Version,
		"reason":           "Stop local processing after lost delivery receipt",
		"confirm":          false,
	}
	code, _ := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	body["confirm"] = true
	body["expected_version"] = before.Task.Version + 1
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusConflict, code)
	body["expected_version"] = before.Task.Version
	code, data := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(data))
	code, replayed := adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusOK, code, string(replayed))
	var first, second db.TaskResolution
	require.NoError(t, json.Unmarshal(data, &first))
	require.NoError(t, json.Unmarshal(replayed, &second))
	require.Equal(t, first, second)
	after := adminDiagnostics(t, h, operator, id)
	require.Equal(t, "closed", after.Task.State)
	require.Equal(t, before.Task.Version+1, after.Task.Version)
	require.Len(t, after.Resolutions, 1)
	require.Equal(t, "delivery_unknown", after.Deliveries[0].State)
	body["reason"] = "Different operation intent"
	code, _ = adminRequest(t, h, operator, http.MethodPost, path, body)
	require.Equal(t, http.StatusConflict, code)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submitText(t, 80, "private-message-canary"))
	current, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "closed", current.State)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 1)
	require.Len(t, p.runs, 1)
}

func TestAdminClosureDoesNotOverrideLiveLeaseOrKnownOutcome(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := unknownTask(t, h)
	operator := taskAdminToken(t, h, id)
	before := adminDiagnostics(t, h, operator, id)
	leased, err := h.store.Claim(context.Background(), id, "test-live-worker", time.Minute)
	require.NoError(t, err)
	body := map[string]any{
		"operation_id":     "close-lease",
		"expected_version": before.Task.Version,
		"reason":           "Close uncertain task",
		"confirm":          true,
	}
	code, _ := adminRequest(t, h, operator, http.MethodPost, "/internal/admin/tasks/"+id.String()+"/close", body)
	require.Equal(t, http.StatusConflict, code)
	require.NoError(t, h.store.Release(context.Background(), id, "test-live-worker", leased.Fence))
	known := h.submitText(t, 82, "Known completed task")
	h.await(t, known, "completed")
	h.awaitReplies(t, 2)
	completed := adminDiagnostics(t, h, operator, known)
	body["expected_version"] = completed.Task.Version
	code, _ = adminRequest(t, h, operator, http.MethodPost, "/internal/admin/tasks/"+known.String()+"/close", body)
	require.Equal(t, http.StatusConflict, code)
	require.Empty(t, adminDiagnostics(t, h, operator, id).Resolutions)
}

func TestAdminClosurePreservesUnknownAuthorizationAcrossRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseAuth = true
	h := setup(t, p)
	id := h.submitText(t, 83, "Lost authorization response")
	h.await(t, id, "auth_unknown")
	operator := taskAdminToken(t, h, id)
	h.stop(t)
	before := adminDiagnostics(t, h, operator, id)
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/admin/tasks/"+id.String()+"/close",
		map[string]any{
			"operation_id": "close-auth", "expected_version": before.Task.Version,
			"reason": "Stop after uncertain authorization", "confirm": true,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	h.start(t)
	require.Equal(t, id, h.submitText(t, 83, "Lost authorization response"))
	after := adminDiagnostics(t, h, operator, id)
	require.Equal(t, "closed", after.Task.State)
	require.Len(t, after.Resolutions, 1)
	require.Equal(t, "auth_unknown", after.Resolutions[0].PreviousState)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
}
