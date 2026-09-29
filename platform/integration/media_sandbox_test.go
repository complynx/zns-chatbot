package integration_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestSandboxPhotoCaptionAndDownload(t *testing.T) {
	t.Parallel()
	f := setup(t)
	var content bytes.Buffer
	require.NoError(t, png.Encode(&content, image.NewRGBA(image.Rect(0, 0, 24, 32))))
	caption := "Что делать с этим? / What is this?"
	query := url.Values{"user": {"101"}, "filename": {"synthetic.png"}, "caption": {caption}}
	response := sandboxMediaRequest(t, f, "/lab/photo?"+query.Encode(), content.Bytes())
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var update telegram.Update
	require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
	require.NotNil(t, update.Message)
	assert.Nil(t, update.Message.Document)
	assert.Equal(t, caption, update.Message.Caption)
	require.Len(t, update.Message.Photo, 1)
	assert.Equal(t, 24, update.Message.Photo[0].Width)
	assert.Equal(t, 32, update.Message.Photo[0].Height)
	file, err := telegram.PhotoDocument(update.Message.Photo)
	require.NoError(t, err)
	body, err := f.b.TG.Download(t.Context(), file)
	require.NoError(t, err)
	assert.Equal(t, content.Bytes(), body)
	for _, sample := range []struct {
		user   string
		status int
	}{{"101", http.StatusOK}, {"202", http.StatusNotFound}} {
		r, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet,
			f.fake.URL+"/lab/files/"+file.FileID+"?user="+sample.user, nil)
		require.NoError(t, requestErr)
		got, requestErr := f.fake.Client().Do(r)
		require.NoError(t, requestErr)
		payload, readErr := io.ReadAll(got.Body)
		require.NoError(t, got.Body.Close())
		require.NoError(t, readErr)
		assert.Equal(t, sample.status, got.StatusCode)
		if sample.status == http.StatusOK {
			assert.Equal(t, content.Bytes(), payload)
			assert.Equal(t, "image/png", got.Header.Get("Content-Type"))
			assert.Equal(t, "nosniff", got.Header.Get("X-Content-Type-Options"))
		}
	}
	for _, route := range []string{"/lab/document?", "/lab/photo?"} {
		query.Set("caption", strings.Repeat("я", 1025))
		got := sandboxMediaRequest(t, f, route+query.Encode(), content.Bytes())
		require.NoError(t, got.Body.Close())
		assert.Equal(t, http.StatusBadRequest, got.StatusCode)
	}
	query.Set("caption", caption)
	got := sandboxMediaRequest(t, f, "/lab/photo?"+query.Encode(), []byte("not an image"))
	require.NoError(t, got.Body.Close())
	assert.Equal(t, http.StatusBadRequest, got.StatusCode)
	got = sandboxMediaRequest(t, f, "/lab/document?"+query.Encode(), []byte("arbitrary document"))
	defer got.Body.Close()
	require.Equal(t, http.StatusOK, got.StatusCode)
	var document telegram.Update
	require.NoError(t, json.NewDecoder(got.Body).Decode(&document))
	assert.Equal(t, caption, document.Message.Caption)
	require.NotNil(t, document.Message.Document)
	assert.Empty(t, document.Message.Photo)
}

func sandboxMediaRequest(t *testing.T, f *fixture, path string, body []byte) *http.Response {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.fake.URL+path, bytes.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("X-Sandbox", "1")
	response, err := f.fake.Client().Do(r)
	require.NoError(t, err)
	return response
}
