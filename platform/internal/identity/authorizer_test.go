package identity_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func authorizerFixture(t *testing.T) (*identity.Authorizer, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	jwk, err := jwkset.NewJWKFromKey(
		&key.PublicKey,
		jwkset.JWKOptions{Metadata: jwkset.JWKMetadataOptions{KID: "synthetic", ALG: jwkset.AlgRS256}},
	)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk.Marshal()}})
	}))
	t.Cleanup(server.Close)
	verifier, err := identity.NewAuthorizer(
		t.Context(),
		identity.AuthorizerConfig{
			Issuer:         "https://relay.invalid",
			JWKSURL:        server.URL,
			BotID:          77,
			AllowLocalHTTP: true,
		},
	)
	require.NoError(t, err)
	return verifier, key
}

func authorizerClaims() jwt.MapClaims {
	now := time.Now().Unix()
	return jwt.MapClaims{
		"iss":                            "https://relay.invalid",
		"aud":                            "zns-identity-provisioner",
		"purpose":                        "telegram_identity_link",
		"sub":                            "telegram:77:opaque-oidc-sub",
		"iat":                            now,
		"nbf":                            now,
		"exp":                            now + 60,
		"jti":                            "synthetic-request",
		"urn:zitadeltg:telegram:bot_id":  "77",
		"urn:zitadeltg:telegram:subject": "opaque-oidc-sub",
		"urn:zitadeltg:telegram:user_id": "95101",
		"given_name":                     "Synthetic",
		"family_name":                    "Person",
		"language_code":                  "ru",
	}
}

func TestAuthorizerVerifiesExactSignedIdentity(t *testing.T) {
	t.Parallel()
	verifier, key := authorizerFixture(t)
	for _, test := range []struct {
		name, claim string
		value       any
	}{
		{"valid", "", nil}, {"issuer", "iss", "https://other.invalid"}, {"purpose", "purpose", "login"},
		{"audience", "aud", "other"}, {"extraAudience", "aud", []string{"zns-identity-provisioner", "other"}},
		{"bot", "urn:zitadeltg:telegram:bot_id", "88"}, {"leadingZeroID", "urn:zitadeltg:telegram:user_id", "095101"},
		{"numericID", "urn:zitadeltg:telegram:user_id", 95101}, {"negativeID", "urn:zitadeltg:telegram:user_id", "-1"},
		{"subject", "sub", "telegram:77:95101"}, {"missingOIDCSubject", "urn:zitadeltg:telegram:subject", ""},
		{"expired", "exp", time.Now().Add(-time.Minute).Unix()}, {"longTTL", "exp", time.Now().Add(time.Hour).Unix()},
		{"future", "iat", time.Now().Add(time.Hour).Unix()}, {"missingIssued", "iat", nil}, {"missingNotBefore", "nbf", nil}, {"missingJTI", "jti", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			claims := authorizerClaims()
			if test.claim != "" {
				claims[test.claim] = test.value
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "synthetic"
			signed, err := token.SignedString(key)
			require.NoError(t, err)
			result, err := verifier.Verify(t.Context(), signed)
			if test.name != "valid" {
				require.ErrorIs(t, err, identity.ErrAuthorizer)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(95101), result.Telegram.ID)
			require.Equal(t, "telegram:77:opaque-oidc-sub", result.Subject)
			require.Equal(t, "ru", result.Telegram.Language)
		})
	}
}

func TestAuthorizerRejectsAlgorithmSignatureAndOversize(t *testing.T) {
	t.Parallel()
	verifier, key := authorizerFixture(t)
	hmac, err := jwt.NewWithClaims(jwt.SigningMethodHS256, authorizerClaims()).SignedString([]byte("synthetic"))
	require.NoError(t, err)
	_, err = verifier.Verify(t.Context(), hmac)
	require.ErrorIs(t, err, identity.ErrAuthorizer)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, authorizerClaims())
	token.Header["kid"] = "synthetic"
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	parts := strings.Split(signed, ".")
	parts[1] = "e30"
	_, err = verifier.Verify(t.Context(), strings.Join(parts, "."))
	require.ErrorIs(t, err, identity.ErrAuthorizer)
	_, err = verifier.Verify(t.Context(), strings.Repeat("x", identity.MaxAuthorizerAssertionBytes+1))
	require.ErrorIs(t, err, identity.ErrAuthorizer)
}

func TestAuthorizerDoesNotFollowJWKSTrustRedirect(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	target := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusOK) }),
	)
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) },
		),
	)
	t.Cleanup(redirect.Close)
	_, err := identity.NewAuthorizer(
		t.Context(),
		identity.AuthorizerConfig{
			Issuer:         "https://relay.invalid",
			JWKSURL:        redirect.URL,
			BotID:          77,
			AllowLocalHTTP: true,
		},
	)
	require.ErrorIs(t, err, identity.ErrAuthorizer)
	require.Zero(t, hits.Load())
}
