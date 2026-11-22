package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestExternalAgentSettingsRespectCapabilityAndConsentVersion(t *testing.T) {
	t.Parallel()
	const resource = "https://loom.example/mcp"
	const capabilities = `{"external_agent":{"version":1,"resource":"` + resource +
		`","access":["history","skills","writes"]}}`
	var mu sync.Mutex
	var profile loom.ExternalAgentProfile
	advertised := false
	writes := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "Bearer service", r.Header.Get("Authorization"))
		assert.Equal(t, "owner", r.Header.Get("X-Subject-Token"))
		assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/capabilities":
			if advertised {
				_, _ = w.Write([]byte(capabilities))
			} else {
				_, _ = w.Write([]byte("{}"))
			}
		case "GET /v1/users/me/external-agent":
			assert.True(t, advertised)
			assert.NoError(t, json.NewEncoder(w).Encode(profile))
		case "PUT /v1/users/me/external-agent":
			var change loom.ExternalAgentProfile
			if json.NewDecoder(r.Body).Decode(&change) != nil || change.Version != profile.Version {
				http.Error(w, "invalid update", http.StatusConflict)
				return
			}
			writes++
			profile = change
			profile.Version++
			if change.History {
				// The write succeeded, but its receipt did not reach Zinusya.
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
				return
			}
			assert.NoError(t, json.NewEncoder(w).Encode(profile))
		default:
			t.Errorf("unexpected Loom request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	client, err := loom.New(remote.URL, remote.Client())
	require.NoError(t, err)
	service := &Service{loom: client, options: Options{ExternalAgentWrites: true}}
	auth := loom.Auth{ServiceToken: "service", SubjectToken: "owner"}
	text, keyboard := service.externalAgentView(t.Context(), auth, nil, false)
	require.Equal(t, externalAgentUnavailable(false), text)
	require.Nil(t, keyboard)
	mu.Lock()
	advertised = true
	mu.Unlock()
	text, keyboard = service.externalAgentView(t.Context(), auth, nil, false)
	require.Contains(t, text, resource)
	require.Len(t, keyboard.InlineKeyboard, 3)
	require.Equal(t, "⬜ Read", keyboard.InlineKeyboard[0][0].Text)
	require.Equal(t, "⬜ Changes", keyboard.InlineKeyboard[1][0].Text)
	oldButton := externalAgentButton(t, "mcp:0:enable")
	notice, refreshed := service.externalAgentView(t.Context(), auth, oldButton, false)
	require.Contains(t, notice, "out of date")
	require.Equal(t, "⬜ Read", refreshed.InlineKeyboard[0][0].Text)
	mu.Lock()
	require.Zero(t, writes)
	require.Equal(t, loom.ExternalAgentProfile{}, profile)
	mu.Unlock()
	enabled := externalAgentButton(t, keyboard.InlineKeyboard[0][0].CallbackData)
	text, keyboard = service.externalAgentView(t.Context(), auth, enabled, false)
	require.Contains(t, text, resource)
	require.Len(t, keyboard.InlineKeyboard, 4)
	require.Equal(t, "☑️ Read", keyboard.InlineKeyboard[0][0].Text)
	require.Equal(t, "mcp:1:disable", keyboard.InlineKeyboard[0][0].CallbackData)
	text, _ = service.externalAgentView(t.Context(), auth, &externalAgentChange{Version: 1, Action: "memory"}, false)
	require.Contains(t, text, "out of date")
	write := externalAgentButton(t, keyboard.InlineKeyboard[1][0].CallbackData)
	text, keyboard = service.externalAgentView(t.Context(), auth, write, false)
	require.NotContains(t, text, "couldn't")
	require.Equal(t, "☑️ Changes", keyboard.InlineKeyboard[1][0].Text)
	button := keyboard.InlineKeyboard[3][0]
	require.Equal(t, "Disable", button.Text)
	require.Equal(t, "mcp:2:disable", button.CallbackData)
	text, _ = service.externalAgentView(t.Context(), auth, write, false)
	require.Contains(t, text, "out of date")
	disabled := externalAgentButton(t, button.CallbackData)
	text, keyboard = service.externalAgentView(t.Context(), auth, disabled, true)
	require.Contains(t, text, resource)
	require.Len(t, keyboard.InlineKeyboard, 3)
	require.Equal(t, "⬜ Чтение", keyboard.InlineKeyboard[0][0].Text)
	require.Equal(t, "⬜ Изменения", keyboard.InlineKeyboard[1][0].Text)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 3, writes)
	require.Equal(t, loom.ExternalAgentProfile{Version: 3}, profile)
}

func externalAgentButton(t *testing.T, data string) *externalAgentChange {
	t.Helper()
	callback := &telegram.CallbackQuery{
		From:    telegram.User{ID: 7},
		Message: &telegram.Message{ID: 42, Chat: telegram.Chat{ID: 7, Type: "private"}},
		Data:    data,
	}
	update := telegram.Update{ID: 1, CallbackQuery: callback}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	request, err := normalize(update, raw)
	require.NoError(t, err)
	require.Equal(t, kindStructured, request.Kind)
	require.Equal(t, externalAgentCommand, request.StructuredCommand)
	require.NotNil(t, request.ExternalAgentChange)
	callback.Message.Chat.ID = 8
	_, err = normalize(update, raw)
	require.ErrorIs(t, err, ErrUnsupportedUpdate)
	return request.ExternalAgentChange
}
