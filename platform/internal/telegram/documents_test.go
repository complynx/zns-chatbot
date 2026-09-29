package telegram_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDocumentDownloadBoundsAndPaths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, path string
		body       []byte
		size       int64
		valid      bool
	}{
		{name: "valid", path: "documents/a.pdf", body: []byte("receipt"), valid: true},
		{name: "traversal", path: "../a.pdf", body: []byte("receipt")},
		{name: "empty", path: "documents/a.pdf"},
		{name: "metadata limit", path: "documents/a.pdf", size: telegram.MaxDocumentBytes + 1},
		{name: "actual limit", path: "documents/a.pdf", body: bytes.Repeat([]byte("x"), telegram.MaxDocumentBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("POST /botsecret/getFile", func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).
					Encode(map[string]any{"ok": true, "result": telegram.File{Path: test.path, Size: test.size}})
			})
			mux.HandleFunc(
				"GET /file/botsecret/documents/a.pdf",
				func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(test.body) },
			)
			server := httptest.NewServer(mux)
			defer server.Close()
			client := telegram.Client{Base: server.URL, Token: "secret"}
			body, err := client.Download(t.Context(), telegram.Document{FileID: "opaque"})
			if test.valid {
				require.NoError(t, err)
				assert.Equal(t, test.body, body)
			} else {
				require.ErrorIs(t, err, telegram.ErrInvalidDocument)
			}
		})
	}
}

func TestDocumentDownloadDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /botsecret/getFile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": telegram.File{Path: "receipt"}})
	})
	mux.HandleFunc(
		"GET /file/botsecret/receipt",
		func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/unexpected", http.StatusFound) },
	)
	mux.HandleFunc(
		"GET /unexpected",
		func(_ http.ResponseWriter, _ *http.Request) { t.Error("download followed redirect") },
	)
	server := httptest.NewServer(mux)
	defer server.Close()
	_, err := (telegram.Client{Base: server.URL, Token: "secret"}).Download(
		t.Context(),
		telegram.Document{FileID: "opaque"},
	)
	require.ErrorIs(t, err, telegram.ErrInvalidDocument)
}
