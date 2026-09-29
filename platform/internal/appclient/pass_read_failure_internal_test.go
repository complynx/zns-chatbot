package appclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestRegistrationHTTPFailedDecodeHasNoResult(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, capabilities, tiers, batch string }{
		{"malformed", `{"export":true,"actions":[123]}`, `{"event":"dance","tiers":123}`, `[{"telegram_id":101},{"telegram_id":"bad"}]`},
		{"oversized", `{"export":true}` + strings.Repeat(" ", MaxAPIBytes), `{"event":"dance"}` + strings.Repeat(" ", MaxAPIBytes), `[{"telegram_id":101}]` + strings.Repeat(" ", MaxAPIBytes)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := test.tiers
				if strings.HasSuffix(r.URL.Path, "tool-capabilities") {
					body = test.capabilities
				}
				if strings.HasSuffix(r.URL.Path, "batches") {
					body = test.batch
				}
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(server.Close)
			client := Client{Base: server.URL, HTTP: server.Client(), SandboxToken: (identity.Signer{}).Token}
			capabilities, err := client.PassToolCapabilities(t.Context(), "bob")
			require.Error(t, err)
			require.Empty(t, capabilities)
			tiers, err := client.PassTierStatus(t.Context(), "bob", "dance")
			require.Error(t, err)
			require.Empty(t, tiers)
			batch, err := client.RunPassBatch(t.Context(), "bob", passbooking.RuntimeBatch{})
			require.Error(t, err)
			require.Nil(t, batch)
		})
	}
}
