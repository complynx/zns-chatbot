package bot

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestRefundCommandPreservesDatabaseFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		status   int
		database bool
	}{
		{"SQL unavailable", http.StatusServiceUnavailable, true},
		{"SQL with domain status", http.StatusForbidden, true},
		{"domain denial", http.StatusForbidden, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var preferenceReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost && r.URL.Path == "/v1/order-refunds/confirm" {
					if test.database {
						w.Header().Set(core.DatabaseFailureHeader, "1")
					}
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(`{"code":"forbidden"}`))
					return
				}
				preferenceReads.Add(1)
				_, _ = w.Write([]byte(`{"language":"en"}`))
			}))
			t.Cleanup(server.Close)
			b := &Bot{API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}}
			message, err := b.handleRefundCommand(t.Context(), incoming{owner: "alice"}, 1,
				orders.Command{Name: refundConfirmAction, EventID: b.currentOrderEvent(), OrderID: "1", Version: 1})
			if test.database {
				require.ErrorIs(t, err, core.ErrDatabase)
				require.Empty(t, message)
				require.Zero(t, preferenceReads.Load())
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, message)
				require.EqualValues(t, 1, preferenceReads.Load())
			}
		})
	}
}
