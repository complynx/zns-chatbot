package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestAgentRegistrationWriteReconcilesOwnerReceiptAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, operator := registrationUser(t, h, 9401, true)
	userID, user := registrationUser(t, h, 9402, false)
	_, other := registrationUser(t, h, 9403, false)
	event := registrationEvent(t, h, operator)
	operation := uuid.NewString()
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"run_id":           uuid.NewString(),
		"arguments":        map[string]any{"event_id": event.ID, "role": "leader"},
		"idempotency_key":  operation,
		"confirmation":     map[string]any{"mode": "none"},
	}
	code, data := adminRequest(t, h, user, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusOK, code, string(data))
	var response struct {
		Status      string                     `json:"status"`
		OperationID string                     `json:"operation_id"`
		Result      registrations.Registration `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, "succeeded", response.Status)
	require.Equal(t, operation, response.OperationID)
	require.Equal(t, userID, response.Result.UserID)
	original := response.Result
	h.stop(t)
	h.start(t)
	lookup := map[string]any{"protocol_version": 1, "operation_id": operation}
	code, data = adminRequest(t, h, user, http.MethodPost, "/internal/agent/registrations/status", lookup)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, original, response.Result)
	code, data = adminRequest(t, h, user, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, original, response.Result)
	code, data = adminRequest(t, h, other, http.MethodPost, "/internal/agent/registrations/status", lookup)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	require.NotContains(t, string(data), original.ID.String())
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/registrations/me",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "read",
			"arguments":        map[string]any{"event_id": event.ID},
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, original, response.Result)
	body["idempotency_key"] = "different"
	code, _ = adminRequest(t, h, user, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusBadRequest, code)
	body["idempotency_key"] = operation
	body["arguments"] = map[string]any{"event_id": event.ID, "role": "follower"}
	code, data = adminRequest(t, h, user, http.MethodPost, "/internal/agent/registrations/join", body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	require.Contains(t, string(data), "terminal_rejection")
	require.NotContains(t, string(data), "\"result\"")
	// An assignment receipt is not a successful join receipt for the same actor.
	adminEntry := registerForEvent(t, h, operator, event.ID, "admin-join", "follower")
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		assignmentPath(event.ID, adminEntry.ID),
		assignmentBody("admin-assign", event, adminEntry),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/registrations/status",
		map[string]any{"protocol_version": 1, "operation_id": "admin-assign"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
}
