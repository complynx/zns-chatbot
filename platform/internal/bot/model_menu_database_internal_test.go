package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestOptionalModelMenuPreservesDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		status   int
		database bool
	}{
		{"database unavailable", http.StatusServiceUnavailable, true},
		{"database with domain status", http.StatusForbidden, true},
		{"ordinary ACL denial", http.StatusForbidden, false},
		{"ordinary provider outage", http.StatusServiceUnavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if test.database {
					w.Header().Set(core.DatabaseFailureHeader, "1")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"code":"unavailable"}`))
			}))
			t.Cleanup(server.Close)
			b := Bot{API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}}
			payload := telegram.Send{}
			err := b.addModelSettingsMenu(t.Context(), "alice", "en", &payload)
			if test.database {
				require.ErrorIs(t, err, core.ErrDatabase)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, payload.Markup.Rows, "failed permission checks never add controls")
		})
	}
}
