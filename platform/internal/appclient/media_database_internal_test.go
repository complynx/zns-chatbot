package appclient

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestMediaFileDatabaseFailureAfterMetadata(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name, body       string
		status           int
		header, database bool
	}{
		{"database", `{"code":"internal_error"}`, 500, true, true},
		{"marked denial", `{"code":"forbidden"}`, 403, true, true},
		{"absent", `{"code":"media_not_found"}`, 404, false, false},
		{"provider", `{"code":"internal_error"}`, 500, false, false},
		{"malformed marker", `private diagnostic`, 500, true, false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			var metadata, downloads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/media/item" {
					metadata.Add(1)
					_, _ = w.Write([]byte(`{"id":"item"}`))
					return
				}
				downloads.Add(1)
				if sample.header {
					w.Header().Set(core.DatabaseFailureHeader, "1")
				}
				w.WriteHeader(sample.status)
				_, _ = w.Write([]byte(sample.body))
			}))
			t.Cleanup(server.Close)
			client := Client{
				Base:         server.URL,
				HTTP:         server.Client(),
				SandboxToken: func(string) string { return "test" },
			}
			_, err := client.Media(t.Context(), "alice", "item")
			require.Error(t, err)
			require.Equal(t, sample.database, core.IsDatabaseFailure(err))
			require.NotContains(t, err.Error(), "private diagnostic")
			require.Equal(t, int32(1), metadata.Load())
			require.Equal(t, int32(1), downloads.Load())
			if sample.status == http.StatusNotFound {
				var problem *core.ProblemError
				require.ErrorAs(t, err, &problem)
				require.Equal(t, "media_not_found", problem.Code)
			}
		})
	}
}
