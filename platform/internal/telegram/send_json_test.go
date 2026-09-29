package telegram_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSendKeyboardWire(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		rows [][]telegram.Button
		want string
	}{
		{"nil", nil, `[]`},
		{"empty", [][]telegram.Button{}, `[]`},
		{"populated", [][]telegram.Button{{{Text: "Choose", Data: "choice"}}}, `[[{"text":"Choose","callback_data":"choice"}]]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			requests := make(chan json.RawMessage, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var raw json.RawMessage
				if json.NewDecoder(r.Body).Decode(&raw) != nil {
					http.Error(w, "invalid test request", http.StatusBadRequest)
					return
				}
				requests <- raw
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":17}}`))
			}))
			t.Cleanup(server.Close)
			client := telegram.Client{Base: server.URL, Token: "test"}
			payload := telegram.Send{ChatID: 303, Text: "Synthetic", Markup: telegram.Markup{Rows: c.rows}}
			_, err := client.Send(t.Context(), payload)
			require.NoError(t, err)
			payload.MessageID = 17
			require.NoError(t, client.Edit(t.Context(), payload))
			for range 2 {
				var wire struct {
					Markup struct {
						Rows json.RawMessage `json:"inline_keyboard"`
					} `json:"reply_markup"`
				}
				require.NoError(t, json.Unmarshal(<-requests, &wire))
				require.JSONEq(t, c.want, string(wire.Markup.Rows), "edit must explicitly clear an existing keyboard")
			}
			if c.rows == nil {
				require.Nil(t, payload.Markup.Rows)
			}
		})
	}
}

func TestSendKeyboardEncodingLeavesOtherCarriersUnchanged(t *testing.T) {
	t.Parallel()
	message, err := json.Marshal(telegram.Message{})
	require.NoError(t, err)
	require.Contains(t, string(message), `"inline_keyboard":null`)
	malformed, err := json.Marshal(telegram.Send{Markup: telegram.Markup{Rows: [][]telegram.Button{nil}}})
	require.NoError(t, err)
	require.Contains(t, string(malformed), `"inline_keyboard":[null]`)
}
