package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestZitadelDelegatedIdentity(t *testing.T) {
	t.Parallel()
	for _, defect := range []string{"none", "inactive", "subject", "issuer", "audience", "client", "expired", "not_yet_valid", "actor", "actor_issuer", "missing_actor"} {
		t.Run(defect, func(t *testing.T) {
			t.Parallel()
			var issuer string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, secret, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "api-client", id)
				assert.Equal(t, "api-secret", secret)
				assert.Equal(t, "/oauth/v2/introspect", r.URL.Path)
				assert.NoError(t, r.ParseForm())
				assert.Equal(t, "user-token", r.PostForm.Get("token"))
				claims := map[string]any{
					"active":    true,
					"sub":       "user-42",
					"iss":       issuer,
					"aud":       []string{"project"},
					"client_id": "bot-client",
					"exp":       time.Now().Add(time.Hour).Unix(),
					"nbf":       time.Now().Add(-time.Minute).Unix(),
					"act":       map[string]string{"sub": "machine", "iss": issuer},
				}
				switch defect {
				case "inactive":
					claims["active"] = false
				case "subject":
					delete(claims, "sub")
				case "issuer":
					claims["iss"] = "https://other.invalid"
				case "audience":
					claims["aud"] = []string{"different"}
				case "client":
					claims["client_id"] = "different"
				case "expired":
					claims["exp"] = time.Now().Add(-time.Hour).Unix()
				case "not_yet_valid":
					claims["nbf"] = time.Now().Add(time.Hour).Unix()
				case "actor":
					claims["act"] = map[string]string{"sub": "other"}
				case "actor_issuer":
					claims["act"] = map[string]string{"sub": "machine", "iss": "https://other.invalid"}
				case "missing_actor":
					delete(claims, "act")
				}
				assert.NoError(t, json.NewEncoder(w).Encode(claims))
			}))
			defer server.Close()
			issuer = server.URL
			client, err := identity.NewZitadel(zitadelTestConfig(server))
			require.NoError(t, err)
			subject, err := client.Verify(t.Context(), "user-token")
			if defect == "none" {
				require.NoError(t, err)
				assert.Equal(t, "user-42", subject)
			} else {
				require.ErrorIs(t, err, identity.ErrZitadelIdentity)
				assert.Empty(t, subject)
			}
		})
	}
}

func TestZitadelExchangeUsesSeparateApp(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "bot-client", id)
		assert.Equal(t, "bot-secret", secret)
		assert.Equal(t, "/oauth/v2/token", r.URL.Path)
		assert.NoError(t, r.ParseForm())
		assert.Equal(t, "mapped-user", r.PostForm.Get("subject_token"))
		assert.Equal(t, "machine-token", r.PostForm.Get("actor_token"))
		assert.Equal(t, "project", r.PostForm.Get("audience"))
		assert.Equal(t, "urn:zitadel:params:oauth:token-type:user_id", r.PostForm.Get("subject_token_type"))
		assert.NoError(
			t,
			json.NewEncoder(w).Encode(map[string]any{"access_token": "issued-token", "token_type": "Bearer",
				"issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": 60}),
		)
	}))
	defer server.Close()
	client, err := identity.NewZitadel(zitadelTestConfig(server))
	require.NoError(t, err)
	token, err := client.Exchange(t.Context(), "mapped-user")
	require.NoError(t, err)
	assert.Equal(t, "issued-token", token)
}

func zitadelTestConfig(server *httptest.Server) identity.ZitadelConfig {
	return identity.ZitadelConfig{
		Issuer:          server.URL,
		Audience:        "project",
		BotClientID:     "bot-client",
		BotClientSecret: "bot-secret",
		APIClientID:     "api-client",
		APIClientSecret: "api-secret",
		ActorID:         "machine",
		ActorToken:      "machine-token",
		HTTP:            server.Client(),
	}
}

func TestSyntheticEmailMatchesAuthorizer(t *testing.T) {
	t.Parallel()
	email, err := identity.SyntheticEmail(123, 456, "telegram.invalid")
	require.NoError(t, err)
	assert.Equal(t, "tg+123+456@telegram.invalid", email)
	for _, domain := range []string{"example.com", "telegram.invalid.evil", "x@telegram.invalid", ".invalid", "UPPER.invalid", "a..invalid"} {
		_, err = identity.SyntheticEmail(123, 456, domain)
		require.Error(t, err, domain)
	}
	_, err = identity.SyntheticEmail(123, 1<<52, "telegram.invalid")
	require.Error(t, err)
}

func TestZitadelOAuthBasicEscapesCredentials(t *testing.T) {
	t.Parallel()
	const clientID = "bot: client+id"
	const secret = "secret:+ %/&"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, url.QueryEscape(clientID), id)
		assert.Equal(t, url.QueryEscape(secret), password)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	config := zitadelTestConfig(server)
	config.BotClientID, config.BotClientSecret = clientID, secret
	client, err := identity.NewZitadel(config)
	require.NoError(t, err)
	_, err = client.Exchange(t.Context(), "mapped-user")
	require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
}
