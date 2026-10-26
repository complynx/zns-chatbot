package telegram_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestChatMenuUsesOnlyExplicitChatScope(t *testing.T) {
	t.Parallel()
	var calls []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if !assert.NoError(t, decoder.Decode(&payload)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls = append(calls, payload)
		switch r.URL.Path {
		case "/bot123:secret/setMyCommands":
			assert.Equal(t, map[string]any{"type": "chat", "chat_id": json.Number("42")}, payload["scope"])
		case "/bot123:secret/setChatMenuButton":
			assert.Equal(t, json.Number("42"), payload["chat_id"])
			assert.Equal(t, map[string]any{"type": "commands"}, payload["menu_button"])
		default:
			t.Errorf("unexpected menu method")
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	commands := []telegram.BotCommand{{Command: "menu", Description: "Menu"}}
	require.NoError(t, client.SetChatCommands(context.Background(), 42, "", commands))
	commands[0].Description = "Меню"
	require.NoError(t, client.SetChatCommands(context.Background(), 42, "ru", commands))
	require.NoError(t, client.SetChatMenuCommands(context.Background(), 42))
	require.Len(t, calls, 3)
	require.NotContains(t, calls[0], "language_code")
	require.Equal(t, "ru", calls[1]["language_code"])
	require.Error(t, client.SetChatCommands(context.Background(), 0, "", commands))
	require.Error(t, client.SetChatMenuCommands(context.Background(), 0))
	require.Len(t, calls, 3, "Invalid input must not use the global Telegram scope")
}

func TestChatMenuUnknownResponseCanReapplySameSettings(t *testing.T) {
	t.Parallel()
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload json.RawMessage
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload)) {
			return
		}
		bodies = append(bodies, append([]byte(nil), payload...))
		if len(bodies) == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if assert.NoError(t, err) {
				assert.NoError(t, connection.Close())
			}
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	err = client.SetChatMenuCommands(context.Background(), 42)
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.Unknown, failure.Outcome)
	require.NoError(t, client.SetChatMenuCommands(context.Background(), 42))
	require.Len(t, bodies, 2)
	require.JSONEq(t, string(bodies[0]), string(bodies[1]))
}

func TestMenuCleanupUsesEmptyListsAndExplicitFallbackScopes(t *testing.T) {
	t.Parallel()
	var calls []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		calls = append(calls, body)
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	for _, scope := range []string{"default", "all_private_chats"} {
		for _, language := range []string{"", "ru"} {
			require.NoError(t, client.ClearDefaultCommands(t.Context(), scope, language))
		}
	}
	require.NoError(t, client.SetChatCommands(t.Context(), 42, "", nil))
	require.Len(t, calls, 5)
	for _, call := range calls {
		require.Equal(t, []any{}, call["commands"])
	}
	require.Equal(t, map[string]any{"type": "default"}, calls[0]["scope"])
	require.Equal(t, map[string]any{"type": "all_private_chats"}, calls[2]["scope"])
	require.Error(t, client.ClearDefaultCommands(t.Context(), "chat", ""))
	require.Error(t, client.ClearDefaultCommands(t.Context(), "default", "en"))
	require.Len(t, calls, 5)
}
