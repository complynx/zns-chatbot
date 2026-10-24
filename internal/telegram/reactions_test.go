package telegram_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestReactionSelectionAndRemovalUseTelegramBooleanReceipt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bot123:fixture/setMessageReaction", r.URL.Path)
		var body struct {
			ChatID    int64                   `json:"chat_id"`
			MessageID int64                   `json:"message_id"`
			Reaction  []telegram.ReactionType `json:"reaction"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.EqualValues(t, 42, body.ChatID)
		assert.EqualValues(t, 7, body.MessageID)
		assert.NotNil(t, body.Reaction)
		if len(body.Reaction) > 0 {
			assert.Equal(t, []telegram.ReactionType{{Type: "emoji", Emoji: "🙈"}}, body.Reaction)
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:fixture", server.Client())
	require.NoError(t, err)
	require.NoError(t, client.SetMessageReaction(t.Context(), 42, 7, "🙈"))
	require.NoError(t, client.SetMessageReaction(t.Context(), 42, 7, ""))
	for _, value := range []string{"custom:42", "not-emoji", "💸"} {
		require.Error(t, client.SetMessageReaction(t.Context(), 42, 7, value))
	}
}

func TestReactionFalseReceiptIsUnknown(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"result":false}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:fixture", server.Client())
	require.NoError(t, err)
	err = client.SetMessageReaction(t.Context(), 42, 7, "👍")
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.Unknown, failure.Outcome)
}
