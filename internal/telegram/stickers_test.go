package telegram_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestCustomEmojiAndSetReadContracts(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]json.RawMessage
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&input)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/bot123:fixture/getCustomEmojiStickers":
			assert.JSONEq(t, `["42"]`, string(input["custom_emoji_ids"]))
			_, _ = fmt.Fprint(w, `{"ok":true,"result":[{"file_id":"file","file_unique_id":"unique",
"custom_emoji_id":"42","type":"custom_emoji","is_video":true,"set_name":"set"}]}`)
		case "/bot123:fixture/getStickerSet":
			assert.JSONEq(t, `"set"`, string(input["name"]))
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"name":"set","title":"A set","sticker_type":"custom_emoji",
"stickers":[{"file_id":"file","file_unique_id":"unique","custom_emoji_id":"42"}]}}`)
		default:
			t.Errorf("unexpected method %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:fixture", server.Client())
	require.NoError(t, err)
	items, err := client.CustomEmojiStickers(t.Context(), []string{"42"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].IsVideo)
	set, err := client.StickerSet(t.Context(), "set")
	require.NoError(t, err)
	require.Len(t, set.Stickers, 1)
}

func TestCustomEmojiRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"ok":true,"result":[{"file_id":"f","custom_emoji_id":"43"}]}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:fixture", server.Client())
	require.NoError(t, err)
	_, err = client.CustomEmojiStickers(t.Context(), []string{"bad"})
	require.Error(t, err)
	require.Zero(t, calls.Load())
	_, err = client.CustomEmojiStickers(t.Context(), []string{"42"})
	require.Error(t, err)
	require.EqualValues(t, 1, calls.Load())
}
