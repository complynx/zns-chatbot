package telegram_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestMenuSettersRequireSuccessfulResult(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"ok":true,"result":false}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	require.Error(t, client.SetDefaultCommandsMenu(t.Context()))
	require.Error(
		t,
		client.SetMyCommands(t.Context(), "", []telegram.BotCommand{{Command: "passes", Description: "Registration"}}),
	)
}
