package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminUtilityFilePreservesDocument(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /botsynthetic/getFile", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).
			Encode(map[string]any{"ok": true, "result": telegram.File{Path: "documents/original.pdf", Size: 9}})
	})
	mux.HandleFunc("GET /file/botsynthetic/documents/original.pdf", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("synthetic"))
	})
	var filename string
	var uploaded []byte
	mux.HandleFunc("POST /botsynthetic/sendDocument", func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("document")
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		filename = header.Filename
		uploaded, err = io.ReadAll(file)
		assert.NoError(t, err)
		assert.Equal(t, "202", r.FormValue("chat_id"))
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	f := registrationPaymentFixture(t)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic", HTTP: server.Client()}
	handleVisible(t, f.b, message(9812, 202, "/get_file opaque_id"))
	assert.Equal(t, "opaque_id.pdf", filename)
	assert.Equal(t, []byte("synthetic"), uploaded)
}
