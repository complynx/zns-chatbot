package bot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestOrderExportUsesCurrentPrincipal(t *testing.T) {
	t.Parallel()
	for _, delegated := range []bool{false, true} {
		t.Run(map[bool]string{false: "sandbox", true: "zitadel"}[delegated], func(t *testing.T) {
			t.Parallel()
			signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/order-events/festival/export", r.URL.Path)
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if delegated {
					assert.Equal(t, "delegated-token", token)
				} else {
					owner, err := signer.Verify(token)
					assert.NoError(t, err)
					assert.Equal(t, "alice", owner)
				}
				_, err := w.Write([]byte("synthetic-workbook"))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			client := APIClient{Base: server.URL, Signer: signer}
			if delegated {
				client.Exchange = &authExchange{}
				client.Links = authLinks{user: identity.User{Owner: "alice", Subject: "z-alice"}}
			}
			ctx, owner, err := client.AuthenticateTelegram(t.Context(), 101)
			require.NoError(t, err)
			body, err := client.ExportOrders(ctx, owner, "festival")
			require.NoError(t, err)
			assert.Equal(t, "synthetic-workbook", string(body))
			if delegated {
				_, err = client.ExportOrders(t.Context(), owner, "festival")
				require.ErrorIs(t, err, identity.ErrZitadelIdentity)
				_, err = client.ExportOrders(ctx, "bob", "festival")
				require.ErrorIs(t, err, identity.ErrZitadelIdentity)
			}
		})
	}
}
