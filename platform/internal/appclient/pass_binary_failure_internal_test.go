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

func TestRegistrationBinaryFailedDecodeHasNoResult(t *testing.T) {
	t.Parallel()
	const envelopeLimit = passbooking.MaxExportBytes*4/3 + passbooking.MaxExportAuthorityBytes
	for _, test := range []struct{ name, proof, snapshot string }{
		{"malformed", `{"id":"proof","filename":123}`, `{"body":"eA==","events":[123]}`},
		{"oversized", `{"id":"proof"}` + strings.Repeat(" ", MaxAPIBytes), `{"body":"eA==","events":["dance"]}` + strings.Repeat(" ", envelopeLimit)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := test.proof
				if strings.HasSuffix(r.URL.Path, "export-snapshot") {
					body = test.snapshot
				}
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(server.Close)
			signer := identity.Signer{}
			client := Client{Base: server.URL, HTTP: server.Client(), SandboxToken: signer.Token}
			proof, err := client.UploadPassProof(t.Context(), "alice", "proof.txt", []byte("synthetic"))
			require.Error(t, err)
			require.Empty(t, proof)
			host := Host{Base: server.URL, HTTP: server.Client(), UserToken: client.UserToken, Signer: signer}
			snapshot, err := host.ExportPassSnapshot(t.Context(), "alice")
			require.Error(t, err)
			require.Empty(t, snapshot)
		})
	}
}
