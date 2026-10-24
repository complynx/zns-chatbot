package config_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func prelinkEnv(t *testing.T) string {
	t.Helper()
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	for name, value := range map[string]string{
		"ISSUER": "https://provider.invalid/tg", "JWKS_URL": "https://provider.invalid/tg/keys",
		"AUDIENCE": "zinusya-prelink", "PROVIDER_ID": "fixture-idp", "SERVICE_BEARER": key,
	} {
		t.Setenv("SHUTTLE_TELEGRAM_PRELINK_"+name, value)
	}
	return key
}

//nolint:paralleltest // prelinkEnv changes process environment.
func TestTelegramPrelinkStartupUsesEnvironmentAndDedicatedScope(t *testing.T) {
	key := prelinkEnv(t)
	c, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
	require.Equal(t, "https://provider.invalid/tg", c.TelegramPrelink.Issuer)
	require.Equal(t, "https://provider.invalid/tg/keys", c.TelegramPrelink.JWKSURL)
	require.Equal(t, "zinusya-prelink", c.TelegramPrelink.Audience)
	require.Equal(t, "fixture-idp", c.TelegramPrelink.ProviderID)
	require.Equal(t, key, c.TelegramPrelink.ServiceBearer)
}

func TestTelegramPrelinkStartupRejectsIncompleteOrSharedTrust(t *testing.T) {
	for _, tc := range []struct{ name, env, value string }{
		{"missing issuer", "ISSUER", ""}, {"missing JWKS", "JWKS_URL", ""},
		{"missing audience", "AUDIENCE", ""}, {"missing provider", "PROVIDER_ID", ""},
		{"missing key", "SERVICE_BEARER", ""}, {"short key", "SERVICE_BEARER", "secret-do-not-print"},
		{"plaintext issuer", "ISSUER", "http://localhost/tg"},
		{"plaintext JWKS", "JWKS_URL", "http://localhost/keys"},
		{"URL credential", "JWKS_URL", "https://secret-do-not-print@provider.invalid/keys"},
		{"query", "JWKS_URL", "https://provider.invalid/keys?secret-do-not-print"},
		{"empty query", "ISSUER", "https://provider.invalid?"},
		{"shared API audience", "AUDIENCE", "example-zinusya-project"},
		{"shared Loom audience", "AUDIENCE", "example-loom-project"},
		{"provider whitespace", "PROVIDER_ID", "fixture idp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := prelinkEnv(t)
			t.Setenv("SHUTTLE_TELEGRAM_PRELINK_"+tc.env, tc.value)
			_, err := config.Load(configFile(t, ""))
			require.Error(t, err)
			require.NotContains(t, err.Error(), key)
			require.NotContains(t, err.Error(), "secret-do-not-print")
		})
	}
	for _, env := range []string{
		"IDENTITY_ACTOR_CLIENT_SECRET",
		"IDENTITY_CLIENT_SECRET",
		"IDENTITY_INTROSPECTION_CLIENT_SECRET",
		"TELEGRAM_WEBHOOK_SECRET",
	} {
		t.Run("shared-"+env, func(t *testing.T) {
			key := prelinkEnv(t)
			t.Setenv("SHUTTLE_"+env, key)
			_, err := config.Load(configFile(t, ""))
			require.Error(t, err)
			require.NotContains(t, err.Error(), key)
		})
	}
}
