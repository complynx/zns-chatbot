package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSandboxStickerAndEmojiPersistence(t *testing.T) {
	t.Parallel()
	f := setup(t)
	// The fake admits format signatures; decoding is exercised by media-worker tests.
	body := []byte("RIFF\x10\x00\x00\x00WEBPVP8 synthetic")
	query := url.Values{"user": {"101"}, "filename": {"test.webp"}}
	response := sandboxMediaRequest(t, f, "/lab/sticker?"+query.Encode(), body)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var first telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&first))
	require.NoError(t, response.Body.Close())
	require.NotNil(t, first.Message.Sticker)
	query.Set("caption", "🚀 before 🧩 / 🧩")
	response = sandboxMediaRequest(t, f, "/lab/custom_emoji?"+query.Encode(), body)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var emoji telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&emoji))
	require.NoError(t, response.Body.Close())
	spans, err := telegram.CustomEmojiSpans(emoji.Message.Text, emoji.Message.Entities)
	require.NoError(t, err)
	require.Len(t, spans, 2)
	assert.Equal(t, 10, spans[0].Offset)
	assert.Equal(t, 15, spans[1].Offset)
	assert.Equal(t, spans[0].ID, spans[1].ID)
	restarted, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(restarted.Handler())
	defer server.Close()
	client := telegram.Client{Base: server.URL, Token: "sandbox"}
	stickers, err := client.GetCustomEmojiStickers(t.Context(), []string{spans[0].ID})
	require.NoError(t, err)
	require.Len(t, stickers, 1)
	assert.Equal(t, first.Message.Sticker.UniqueID, stickers[0].UniqueID)
	for _, user := range []string{"101", "202"} {
		request, requestErr := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			server.URL+"/lab/files/"+stickers[0].FileID+"?user="+user+"&preview=1",
			nil,
		)
		require.NoError(t, requestErr)
		got, requestErr := server.Client().Do(request)
		require.NoError(t, requestErr)
		require.NoError(t, got.Body.Close())
		if user == "101" {
			assert.Equal(t, http.StatusOK, got.StatusCode)
		} else {
			assert.Equal(t, http.StatusNotFound, got.StatusCode)
		}
	}
	response = sandboxMediaRequest(t, f, "/lab/custom_emoji?"+query.Encode(), body)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var repeated telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&repeated))
	require.NoError(t, response.Body.Close())
	assert.Equal(t, emoji.Message.Entities, repeated.Message.Entities)
}

func TestSandboxStickerAdmission(t *testing.T) {
	t.Parallel()
	f := setup(t)
	body := []byte("RIFF\x10\x00\x00\x00WEBPVP8 synthetic")
	for _, query := range []string{
		"user=999&filename=test.webp", "user=101&filename=test.png", "user=101&filename=test.webp&is_video=true",
		"user=101&filename=test.webp&caption=no-sticker-caption", "user=101&filename=test.webp&is_animated=bad",
	} {
		response := sandboxMediaRequest(t, f, "/lab/sticker?"+query, body)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	}
	response := sandboxMediaRequest(t, f, "/lab/custom_emoji?user=101&filename=test.webp&caption=no-marker", body)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	response = sandboxMediaRequest(
		t,
		f,
		"/lab/sticker?user=101&filename=test.webp",
		[]byte(strings.Repeat("x", (1<<20)+1)),
	)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
}
