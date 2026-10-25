package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestSendChatActionTypingAndCancellation(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/bot123:secret/sendChatAction", r.URL.Path)
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		action := "typing"
		if calls.Load() == 2 {
			action = "choose_sticker"
		}
		assert.Equal(t, map[string]any{"chat_id": float64(42), "action": action}, body)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	require.NoError(t, client.SendChatAction(context.Background(), 42))
	require.NoError(t, client.SendActivity(context.Background(), 42, true))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, client.SendChatAction(ctx, 42))
	require.EqualValues(t, 2, calls.Load())
}
