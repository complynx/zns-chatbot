package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestSendSavedReplyKeyboardsAndRemoval(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ReplyMarkup map[string]json.RawMessage `json:"reply_markup"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Len(t, body.ReplyMarkup, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":7}}}`))
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:fixture", server.Client())
	require.NoError(t, err)
	for _, markup := range []telegram.ReplyMarkup{
		&telegram.ReplyKeyboardMarkup{Keyboard: [][]telegram.KeyboardButton{{{Text: "/menu"}}}},
		&telegram.ReplyKeyboardRemove{RemoveKeyboard: true},
		&telegram.ForceReply{ForceReply: true},
		&telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Form", WebApp: &telegram.WebAppInfo{URL: "https://app.example/app/"}}}}},
		&telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{Text: "Legacy", CallbackData: "fixture"}}}},
	} {
		raw, marshalErr := json.Marshal(telegram.SendMessageRequest{ChatID: 7, Text: "Saved", ReplyMarkup: markup})
		require.NoError(t, marshalErr)
		var restored telegram.SendMessageRequest
		require.NoError(t, json.Unmarshal(raw, &restored))
		restoredRaw, restoredErr := json.Marshal(restored)
		require.NoError(t, restoredErr)
		require.JSONEq(t, string(raw), string(restoredRaw))
		_, sendErr := client.SendMessage(context.Background(), restored)
		require.NoError(t, sendErr)
	}
}
