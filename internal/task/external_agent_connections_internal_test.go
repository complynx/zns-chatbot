package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestExternalAgentConnectionsRevokeOneSeveralOrAll(t *testing.T) {
	t.Parallel()
	const path = "/v1/users/me/external-agent/connections"
	const capabilities = `{"external_agent":{"version":1,"resource":"https://loom.example/mcp",` +
		`"oauth":{"manage_connections":true}}}`
	var mu sync.Mutex
	var revoked [][]string
	var connections []loom.ExternalAgentConnection
	for _, name := range []string{"first", "second", "third", "fourth", "fifth"} {
		connections = append(connections, loom.ExternalAgentConnection{ID: name, Name: name})
	}
	connections[1].Name = connections[0].Name
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "Bearer service", r.Header.Get("Authorization"))
		assert.Equal(t, "owner", r.Header.Get("X-Subject-Token"))
		assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/capabilities":
			_, _ = w.Write([]byte(capabilities))
		case "GET /v1/users/me/external-agent":
			assert.NoError(t, json.NewEncoder(w).Encode(loom.ExternalAgentProfile{Enabled: true, Version: 8}))
		case "GET " + path:
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"connections": connections}))
		case "POST " + path + "/revoke":
			var request struct {
				IDs []string `json:"ids"`
			}
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			revoked = append(revoked, request.IDs)
			connections = slices.DeleteFunc(connections, func(connection loom.ExternalAgentConnection) bool {
				return slices.Contains(request.IDs, connection.ID)
			})
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected Loom request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	service := &Service{loom: client}
	auth := loom.Auth{ServiceToken: "service", SubjectToken: "owner"}
	text, keyboard := service.externalAgentView(t.Context(), auth, nil, false)
	press := func(row int, label string) *externalAgentChange {
		t.Helper()
		button := keyboard.InlineKeyboard[row][0]
		require.Contains(t, button.Text, label)
		require.LessOrEqual(t, len(button.CallbackData), 64)
		return externalAgentButton(t, button.CallbackData)
	}
	apply := func(action *externalAgentChange) {
		text, keyboard = service.externalAgentView(t.Context(), auth, action, false)
	}
	apply(press(2, "Authorized clients"))
	require.NotEqual(t, keyboard.InlineKeyboard[0][0].Text, keyboard.InlineKeyboard[1][0].Text)
	apply(press(0, "first"))
	first := press(6, "Revoke selected (1)")
	apply(first)
	require.Contains(t, text, "Authorization revoked")
	apply(first)
	require.Contains(t, text, "The list changed")
	apply(press(0, "first"))
	apply(press(2, "fourth"))
	apply(press(5, "Revoke selected (2)"))
	apply(press(2, "Select all"))
	apply(press(2, "Revoke selected (2)"))
	require.Contains(t, text, "No active authorizations")
	apply(press(0, "Back to settings"))
	require.Contains(t, text, "External agents (MCP)")
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, [][]string{{"first"}, {"second", "fourth"}, {"third", "fifth"}}, revoked)
}
