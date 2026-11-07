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

func agentAdminRead(t *testing.T, h *harness, token, method string, arguments any) (int, []byte) {
	t.Helper()
	return adminRequest(t, h, token, http.MethodPost, "/internal/agent/administration/"+method, map[string]any{
		"protocol_version": 1, "operation_id": "operator-read", "arguments": arguments,
	})
}

func TestAgentAdministrativeReadsRespectRoleScopeAndRevocation(t *testing.T) {
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
	code, data := adminRequest(
		t,
		h,
		basic,
		http.MethodPost,
		"/internal/agent/administration/discover",
		map[string]any{},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.JSONEq(t, `{"methods":[]}`, string(data))
	for _, method := range []string{
		"settings",
		"tasks",
		"task",
		"pair_notifications",
		"pair_notification",
		"orders_catalog",
		"massage_schedule",
	} {
		arguments := map[string]any{}
		if method == "task" || method == "pair_notification" {
			arguments["id"] = id
		}
		if method == "orders_catalog" || method == "massage_schedule" {
			arguments["event_id"] = uuid.New()
		}
		code, _ = agentAdminRead(t, h, basic, method, arguments)
		require.Equal(t, http.StatusForbidden, code, method)
	}
	operator := taskAdminToken(t, h, id)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/discover",
		map[string]any{},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var discovery struct {
		Methods []struct {
			ID          string          `json:"id"`
			Effect      string          `json:"effect"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(data, &discovery))
	writeMethods := map[string]bool{
		"assign": true, "authority": true, "resolve_notification": true, "store_archive": true,
		"apply_history": true, "set_orders_catalog": true, "set_massage_schedule": true,
		"set_massage_specialist_binding": true, "close_task": true, "update_settings": true,
		"revoke_user": true, "change_event": true,
	}
	methodEffects := make(map[string]string)
	methodIDs := make(map[string]bool)
	for _, method := range discovery.Methods {
		require.False(t, methodIDs[method.ID], "duplicate administrative tool: %s", method.ID)
		methodIDs[method.ID] = true
		require.Contains(t, []string{"read", "write"}, method.Effect, method.ID)
		methodEffects[method.ID] = method.Effect
		if writeMethods[method.ID] {
			require.Equal(t, "write", method.Effect, method.ID)
		}
	}
	for _, required := range []string{"events", "event", "registrations", "assign", "authorities", "authority",
		"resolve_notification", "notifications", "notification", "pair_notifications", "pair_notification",
		"settings", "tasks", "task", "orders_catalog", "set_orders_catalog",
		"massage_schedule", "set_massage_schedule", "close_task", "update_settings",
		"revoke_user", "user", "user_revocation_receipt", "change_event",
	} {
		require.True(t, methodIDs[required], "missing administrative tool: %s", required)
		if !writeMethods[required] {
			require.Equal(t, "read", methodEffects[required], required)
		}
	}
	code, data = agentAdminRead(t, h, operator, "settings", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(data))
	var settings struct {
		Status      string               `json:"status"`
		OperationID string               `json:"operation_id"`
		Result      db.VersionedSettings `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &settings))
	require.Equal(t, "succeeded", settings.Status)
	require.Equal(t, "operator-read", settings.OperationID)
	require.Positive(t, settings.Result.Version)
	code, data = agentAdminRead(t, h, operator, "tasks", map[string]any{"state": "delivery_unknown", "limit": 1})
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), id.String())
	code, _ = agentAdminRead(t, h, operator, "tasks", map[string]any{"limit": 21})
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, data = agentAdminRead(t, h, operator, "task", map[string]any{"id": id})
	require.Equal(t, http.StatusOK, code, string(data))
	for _, private := range []string{"private-message-canary", "payload", "snapshot", "profile", "auth_session_id"} {
		require.NotContains(t, string(data), private)
	}
	foreign, _, err := h.store.Intake(context.Background(), db.IntakeRequest{
		BotID: 124, UpdateID: 81, TelegramUserID: 789, ChatID: 789, Payload: json.RawMessage(`{"kind":"message"}`),
	}, nil)
	require.NoError(t, err)
	code, _ = agentAdminRead(t, h, operator, "task", map[string]any{"id": foreign.ID})
	require.Equal(t, http.StatusNotFound, code)
	code, _ = agentAdminRead(t, h, operator, "task", map[string]any{"id": uuid.New()})
	require.Equal(t, http.StatusNotFound, code)
	code, _ = agentAdminRead(t, h, operator, "task", map[string]any{"id": "invalid"})
	require.Equal(t, http.StatusUnprocessableEntity, code)
	_, err = h.store.SetUserRevoked(context.Background(), user.ID, "operator")
	require.NoError(t, err)
	for _, method := range []string{"settings", "tasks", "task", "pair_notifications", "pair_notification"} {
		arguments := map[string]any{}
		if method == "task" || method == "pair_notification" {
			arguments["id"] = id
		}
		code, _ = agentAdminRead(t, h, operator, method, arguments)
		require.Equal(t, http.StatusForbidden, code, method)
	}
	code, _ = adminRequest(t, h, operator, http.MethodPost, "/internal/agent/administration/discover", map[string]any{})
	require.Equal(t, http.StatusForbidden, code)
}
