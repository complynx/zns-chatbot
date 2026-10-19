package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestFileReadRefreshesPathAndRejectsOversizeBody(t *testing.T) {
	t.Parallel()
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			n := gets.Add(1)
			_ = json.NewEncoder(w).
				Encode(map[string]any{"ok": true, "result": map[string]any{"file_id": "file", "file_path": "docs/" + string('a'+n) + ".pdf"}})
			return
		}
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:test-token", server.Client())
	require.NoError(t, err)
	for range 2 {
		raw, e := client.ReadFile(context.Background(), "file", 4)
		require.NoError(t, e)
		require.Equal(t, []byte("data"), raw)
	}
	require.EqualValues(t, 2, gets.Load())
	_, err = client.ReadFile(context.Background(), "file", 3)
	require.ErrorIs(t, err, telegram.ErrFileTooLarge)
}
func TestFileDownloadNeverFollowsRedirectOrTraversal(t *testing.T) {
	t.Parallel()
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached.Store(true) }))
	t.Cleanup(target.Close)
	for _, path := range []string{"../secrets", "docs/../../secrets", "docs/%2e%2e/secrets", "https://other.invalid/file", "docs/valid.pdf"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					_ = json.NewEncoder(w).
						Encode(map[string]any{"ok": true, "result": map[string]any{"file_id": "file", "file_path": path}})
					return
				}
				http.Redirect(w, r, target.URL, http.StatusFound)
			}))
			defer server.Close()
			client, err := telegram.New(server.URL, "123:test-token", server.Client())
			require.NoError(t, err)
			_, err = client.ReadFile(context.Background(), "file", 4)
			require.ErrorIs(t, err, telegram.ErrFileUnavailable)
			require.NotContains(t, err.Error(), "test-token")
			require.False(t, reached.Load())
		})
	}
}

func TestFileReadClassifiesOnlyExplicitCloudSizeRefusal(t *testing.T) {
	t.Parallel()
	const refusal = `{"ok":false,"error_code":400,"description":"Bad Request: file is too big"}`
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		tooLarge bool
	}{
		{"bad-request", 400, refusal, true},
		{"failure-envelope-on-200", 200, refusal, true},
		{"other-bad-request", 400, `{"ok":false,"error_code":400,"description":"Bad Request: wrong file_id"}`, false},
		{"near-match", 400, `{"ok":false,"error_code":400,"description":"Bad Request: file is too big PRIVATE"}`, false},
		{"rate-limit", 429, `{"ok":false,"error_code":429,"description":"Bad Request: file is too big","parameters":{"retry_after":3}}`, false},
		{"server-error", 500, `{"ok":false,"error_code":500,"description":"Bad Request: file is too big"}`, false},
		{"incoherent-status", 500, refusal, false},
		{"missing-ok", 400, `{"error_code":400,"description":"Bad Request: file is too big"}`, false},
		{"missing-code", 400, `{"ok":false,"description":"Bad Request: file is too big"}`, false},
		{"malformed", 400, `{"ok":false`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var downloads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					downloads.Add(1)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			client, err := telegram.New(server.URL, "123:test-token", server.Client())
			require.NoError(t, err)
			raw, err := client.ReadFile(t.Context(), "file", 4)
			require.Error(t, err)
			require.Nil(t, raw)
			if tc.tooLarge {
				require.ErrorIs(t, err, telegram.ErrFileTooLarge)
				_, sendErr := client.SendMessage(t.Context(), telegram.SendMessageRequest{ChatID: 456, Text: "test"})
				require.Error(t, sendErr)
				require.NotErrorIs(t, sendErr, telegram.ErrFileTooLarge)
				var rejected *telegram.Error
				require.ErrorAs(t, sendErr, &rejected)
				require.Equal(t, telegram.Rejected, rejected.Outcome)
				require.Equal(t, http.StatusBadRequest, rejected.StatusCode)
			} else {
				require.NotErrorIs(t, err, telegram.ErrFileTooLarge)
			}
			require.Zero(t, downloads.Load())
			require.NotContains(t, err.Error(), "test-token")
			require.NotContains(t, err.Error(), "PRIVATE")
			require.NotContains(t, err.Error(), "Bad Request:")
		})
	}
}
