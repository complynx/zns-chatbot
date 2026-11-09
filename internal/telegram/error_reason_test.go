package telegram_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestTelegramReasonClassDoesNotExposeProviderDescription(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ description, reason string }{
		{"Bad Request: chat not found", "chat_not_found"},
		{"Forbidden: bot was blocked by the user", "bot_blocked"},
		{"Forbidden: user is deactivated", "user_deactivated"},
		{"private token and user identifier", "other_rejection"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"ok":false,"error_code":400,"description":%q}`, tc.description)
			}))
			defer server.Close()
			client, err := telegram.New(server.URL, "123:private_token", server.Client())
			require.NoError(t, err)
			err = client.SetChatMenuCommands(t.Context(), 42)
			var failure *telegram.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.reason, failure.ReasonClass())
			require.NotContains(t, failure.Error(), tc.description)
		})
	}
}
