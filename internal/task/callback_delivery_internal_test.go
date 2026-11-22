package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type callbackEditCase struct {
	name, rejection string
	send            bool
	err             bool
}

func TestCallbackEditsOrFallsBackOnlyAfterDefiniteRejection(t *testing.T) {
	t.Parallel()
	for _, tc := range []callbackEditCase{
		{name: "edited"},
		{name: "already edited", rejection: "Bad Request: message is not modified"},
		{name: "deleted", rejection: "Bad Request: message to edit not found", send: true},
		{name: "rate limited", rejection: "rate limited", err: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkCallbackEdit(t, tc)
		})
	}
}

func checkCallbackEdit(t *testing.T, tc callbackEditCase) {
	t.Helper()
	var calls []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/bot123:test/editMessageText" {
			var request telegram.EditMessageRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Equal(t, int64(71), request.MessageID)
			assert.Equal(t, "menu", request.Text)
			assert.Equal(t, "mcp:0:enable", request.ReplyMarkup.InlineKeyboard[0][0].CallbackData)
			if tc.rejection != "" {
				code := http.StatusBadRequest
				if tc.err {
					code = http.StatusTooManyRequests
				}
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": code, "description": tc.rejection})
				return
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":71,"chat":{"id":25}}}`))
	}))
	defer server.Close()
	tg, err := telegram.New(server.URL, "123:test", server.Client())
	require.NoError(t, err)
	s := &Service{tg: tg}
	payload := telegram.SendMessageRequest{ChatID: 25, Text: "menu", ReplyMarkup: &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "read", CallbackData: "mcp:0:enable"}}},
	}}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	i := intent{User: telegram.User{ID: 25}, ChatID: 25, CallbackMessageID: 71}
	rows := editCallbackOutput(i, []db.Outbound{{Payload: raw}})
	_, _, err = s.sendOutbound(t.Context(), db.Delivery{Payload: rows[0].Payload})
	if tc.err {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	expected := []string{"/bot123:test/editMessageText"}
	if tc.send {
		expected = append(expected, "/bot123:test/sendMessage")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, expected, calls)
}

func TestAgentInstructionsUseAdvertisedBrokerSettings(t *testing.T) {
	t.Parallel()
	capability := loom.ExternalAgentCapability{
		Resource: "https://loom.example/mcp",
		OAuth: &loom.ExternalAgentOAuth{
			Issuer: "https://loom.example", Scope: "mcp", DynamicRegistration: true,
		},
	}
	text, keyboard := externalAgentInstructionsView(capability, loom.ExternalAgentProfile{Version: 8}, false)
	require.Contains(t, text, `"zinusya-mcp"`)
	require.Contains(t, text, `"url": "https://loom.example/mcp"`)
	require.Contains(t, text, `"scope": "mcp"`)
	require.Contains(t, text, "PKCE S256")
	require.Equal(t, "mcp:8:settings", keyboard.InlineKeyboard[0][0].CallbackData)
}
