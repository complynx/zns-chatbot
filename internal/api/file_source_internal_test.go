package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestFileSourceFailuresDistinguishRefusalAndRecoverOnFreshRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rejection        string
		status, download, want int
		detail                 string
	}{
		{"invalid-id", "{\"ok\":false,\"error_code\":400,\"description\":\"Bad Request: invalid file_id" +
			"\"}", 400, 0, 404, "file_unavailable"},
		{"invalid-id-on-200", "{\"ok\":false,\"error_code\":400,\"description\":\"Bad Request: invalid file_id" +
			"\"}", 200, 0, 404, "file_unavailable"},
		{"near-match", "{\"ok\":false,\"error_code\":400,\"description\":\"Bad Request: invalid file_id" +
			" PRIVATE\"}", 400, 0, 503, "Telegram file unavailable"},
		{"source-denied", `{"ok":false,"error_code":403,"description":"Forbidden: PRIVATE"}`, 403, 0, 424, "file_source_access_denied"},
		{"source-denied-on-200", `{"ok":false,"error_code":403,"description":"Forbidden: PRIVATE"}`, 200, 0, 424, "file_source_access_denied"},
		{"service-credentials", `{"ok":false,"error_code":401,"description":"Unauthorized PRIVATE"}`, 401, 0, 503, "Telegram file unavailable"},
		{"ambiguous", "{\"ok\":false,\"error_code\":400,\"description\":\"Bad Request: wrong file_id o" +
			"r the file is temporarily unavailable\"}", 400, 0, 503, "Telegram file unavailable"},
		{"rate-limited", `{"ok":false,"error_code":429,"parameters":{"retry_after":3}}`, 429, 0, 503, "Telegram file unavailable"},
		{"malformed-denial", `PRIVATE`, 403, 0, 503, "Telegram file unavailable"},
		{"incoherent-invalid-id", "{\"ok\":false,\"error_code\":400,\"description\":\"Bad Request: invalid file_id" +
			"\"}", 500, 0, 503, "Telegram file unavailable"},
		{"download-missing", "", 0, 404, 503, "Telegram file unavailable"},
		{"download-denied", "", 0, 403, 424, "file_source_access_denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var recovered atomic.Bool
			var gets atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					gets.Add(1)
					if !recovered.Load() && tc.rejection != "" {
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.rejection))
						return
					}
					_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"file","file_path":"docs/source.bin"}}`))
					return
				}
				if !recovered.Load() && tc.download != 0 {
					w.WriteHeader(tc.download)
					_, _ = w.Write([]byte("PRIVATE"))
					return
				}
				_, _ = w.Write([]byte("data"))
			}))
			t.Cleanup(peer.Close)
			client, err := telegram.New(peer.URL, "123:secret-token", peer.Client())
			require.NoError(t, err)
			server := Server{options: Options{Telegram: client}}
			record := db.File{TelegramID: "file"}
			raw, err := server.readFile(t.Context(), record)
			require.Nil(t, raw)
			var response huma.StatusError
			require.ErrorAs(t, err, &response)
			require.Equal(t, tc.want, response.GetStatus())
			require.NotContains(t, err.Error(), "PRIVATE")
			require.NotContains(t, err.Error(), "secret-token")
			require.Contains(t, err.Error(), tc.detail)
			recovered.Store(true)
			raw, err = server.readFile(t.Context(), record)
			require.NoError(t, err)
			require.Equal(t, []byte("data"), raw)
			require.EqualValues(t, 2, gets.Load())
		})
	}
}
