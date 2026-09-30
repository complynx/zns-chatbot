package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestZitadelInactiveLinkInvalidatesComposedCaches(t *testing.T) {
	t.Parallel()
	for _, lookup := range []string{"subject", "telegram"} {
		t.Run(lookup, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			server, unavailable := cacheLinksProvider(t)
			config := identity.ZitadelConfig{
				Issuer: server.URL, Audience: "project", BotClientID: "bot-client", BotClientSecret: "bot-secret",
				APIClientID: "api-client", APIClientSecret: "api-secret", ActorID: "actor", ActorToken: "actor-token",
				Sandbox: true,
			}
			first, err := identity.NewZitadel(config)
			require.NoError(t, err)
			second, err := identity.NewZitadel(config)
			require.NoError(t, err)
			adapters := []*identity.Zitadel{first, second}
			links := identity.Links{
				DB: db, Issuer: server.URL, BotID: 123,
				InvalidateSubject: func(subject string) {
					for _, adapter := range adapters {
						adapter.InvalidateSubject(subject)
					}
				},
			}
			require.NoError(t, links.Bind(t.Context(), "alice", 101, "z-alice"))
			for _, adapter := range adapters {
				for _, subject := range []string{"z-alice", "z-bob"} {
					token, exchangeErr := adapter.Exchange(t.Context(), subject)
					require.NoError(t, exchangeErr)
					_, verifyErr := adapter.Verify(t.Context(), token)
					require.NoError(t, verifyErr)
				}
			}
			_, err = db.Exec(t.Context(), `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
			require.NoError(t, err)
			unavailable.Store(true)
			if lookup == "subject" {
				_, err = links.Subject(t.Context(), "z-alice")
			} else {
				_, err = links.Telegram(t.Context(), 101)
			}
			require.ErrorIs(t, err, identity.ErrZitadelIdentity)
			for _, adapter := range adapters {
				_, err = adapter.Exchange(t.Context(), "z-alice")
				require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
				_, err = adapter.Verify(t.Context(), "token-z-alice")
				require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
				_, err = adapter.Exchange(t.Context(), "z-bob")
				require.NoError(t, err, "unrelated completed delegations remain usable")
				_, err = adapter.Verify(t.Context(), "token-z-bob")
				require.NoError(t, err, "unrelated positive verdicts remain usable")
			}
		})
	}
}

func cacheLinksProvider(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	unavailable := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.ParseForm() != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body := map[string]any{
			"access_token": "token-" + r.Form.Get("subject_token"), "token_type": "Bearer",
			"issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": 3600,
		}
		if r.URL.Path == "/oauth/v2/introspect" {
			body = map[string]any{
				"active": true, "sub": strings.TrimPrefix(r.Form.Get("token"), "token-"),
				"iss": "http://" + r.Host, "aud": []string{"project"}, "client_id": "bot-client",
				"exp": time.Now().Add(time.Hour).Unix(), "act": map[string]string{"sub": "actor"},
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server, unavailable
}
