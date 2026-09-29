package telegram_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestVerifyBot(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, response, message string
	}{
		{"match", `{"ok":true,"result":{"id":101,"is_bot":true}}`, ""},
		{"wrong namespace", `{"ok":true,"result":{"id":202,"is_bot":true}}`, "telegram bot identity does not match configuration"},
		{"human", `{"ok":true,"result":{"id":101,"is_bot":false}}`, "telegram bot identity does not match configuration"},
		{"missing identity", `{"ok":true,"result":{}}`, "telegram bot identity does not match configuration"},
		{"denied", `{"ok":false,"error_code":401,"description":"synthetic-token-canary"}`, "telegram bot identity unavailable"},
		{"malformed", `{"ok":true,"result":{"id":"synthetic-token-canary"}}`, "telegram bot identity unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/botsynthetic-token-canary/getMe", r.URL.Path)
				_, _ = w.Write([]byte(scenario.response))
			}))
			t.Cleanup(server.Close)
			client := telegram.Client{Base: server.URL, Token: "synthetic-token-canary"}
			err := client.VerifyBot(t.Context(), 101)
			if scenario.message == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, scenario.message)
			assert.NotContains(t, err.Error(), client.Token)
		})
	}
}

func TestVerifyBotInvalidConfigurationAndCancellation(t *testing.T) {
	t.Parallel()
	client := telegram.Client{Base: "http://127.0.0.1:1", Token: "synthetic-token-canary"}
	require.EqualError(t, client.VerifyBot(t.Context(), 0), "telegram bot identity requires a positive configured ID")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.EqualError(t, client.VerifyBot(ctx, 101), "telegram bot identity unavailable")
}
