package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/shuttle/internal/config"
)

func configFile(t *testing.T, suffix string) string {
	t.Helper()
	data, err := os.ReadFile("../../config/shuttle.example.yaml")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, append(data, []byte(suffix)...), 0600))
	return path
}
func TestStartupConfigOverridesAndBotNamespace(t *testing.T) {
	t.Setenv("SHUTTLE_TELEGRAM_TOKEN", "789:another_fake_token")
	t.Setenv("SHUTTLE_HTTP_TIMEOUT", "2s")
	t.Setenv("SHUTTLE_LOOM_BASE_URL", "http://loom:8080")
	t.Setenv("SHUTTLE_OTEL_ENDPOINT", "http://alloy:4318")
	c, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
	require.Equal(t, int64(789), c.Telegram.BotID)
	require.Equal(t, 2*time.Second, c.HTTPTimeout)
	require.Equal(t, "http://loom:8080", c.Loom.BaseURL)
	require.Equal(t, "http://alloy:4318", c.OTel.Endpoint)
}
func TestInvalidStartupDoesNotRevealCredentials(t *testing.T) {
	for _, tc := range []struct{ name, env, value string }{
		{"timeout", "SHUTTLE_HTTP_TIMEOUT", "0s"},
		{"shutdown", "SHUTTLE_SHUTDOWN_TIMEOUT", "6s"},
		{"issuer", "SHUTTLE_IDENTITY_ISSUER", "http://evil.example/secret-token"},
		{"userinfo", "SHUTTLE_LOOM_BASE_URL", "https://user:secret-token@loom.example"},
		{"ratio", "SHUTTLE_OTEL_SAMPLE_RATIO", "NaN"},
		{"port", "SHUTTLE_HTTP_ADDRESS", "localhost:99999"},
		{"bot", "SHUTTLE_TELEGRAM_TOKEN", "0:secret-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			_, err := config.Load(configFile(t, ""))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-token")
		})
	}
}
func TestUnknownYAMLAndTrailingDocumentAreRejected(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"\nunknown_runtime_business_rule: true\n", "\n---\nhttp_timeout: 1s\n"} {
		_, err := config.Load(configFile(t, suffix))
		require.Error(t, err)
	}
}
func TestLoopbackIssuerIsExplicitTestSeam(t *testing.T) {
	t.Setenv("SHUTTLE_IDENTITY_ISSUER", "http://127.0.0.1:8888")
	_, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
}
