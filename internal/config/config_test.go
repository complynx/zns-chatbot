package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
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
	t.Setenv("SHUTTLE_MINI_APP_ORIGIN", "https://app.example.invalid")
	c, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
	require.Equal(t, int64(789), c.Telegram.BotID)
	require.Equal(t, 2*time.Second, c.HTTPTimeout)
	require.Equal(t, "http://loom:8080", c.Loom.BaseURL)
	require.Equal(t, "http://alloy:4318", c.OTel.Endpoint)
	require.Equal(t, "https://app.example.invalid", c.MiniAppOrigin)
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
		{"app scheme", "SHUTTLE_MINI_APP_ORIGIN", "http://app.example/secret-token"},
		{"app path", "SHUTTLE_MINI_APP_ORIGIN", "https://app.example/secret-token"},
		{"app credentials", "SHUTTLE_MINI_APP_ORIGIN", "https://user:secret-token@app.example"},
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

func TestMiniAppLocalRedirectStartupConfiguration(t *testing.T) {
	const launch = "https://complynx.net/testbot/18382/app/"
	for _, tc := range []struct {
		name, origin, launch, allow, want string
		valid                             bool
	}{
		{name: "disabled", valid: true},
		{name: "default HTTPS", origin: "https://app.example.invalid", want: "https://app.example.invalid/app/", valid: true},
		{name: "HTTPS launch override", origin: "https://app.example.invalid", launch: launch, want: launch, valid: true},
		{name: "local explicit opt-in", origin: "http://localhost:18382", launch: launch, allow: "true", want: launch, valid: true},
		{name: "HTTP default denied", origin: "http://localhost:18382", launch: launch},
		{name: "local requires HTTPS launch", origin: "http://localhost:18382", allow: "true"},
		{name: "remote HTTP stays denied", origin: "http://evil.invalid", launch: launch, allow: "true"},
		{name: "launch requires origin", launch: launch},
		{name: "launch never HTTP", origin: "http://localhost:18382", launch: "http://localhost:18382/app/", allow: "true"},
		{name: "launch credentials", origin: "https://app.example.invalid", launch: "https://user:secret-token@example.invalid/app/"},
		{name: "bad bool", origin: "https://app.example.invalid", allow: "secret-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHUTTLE_MINI_APP_ORIGIN", tc.origin)
			t.Setenv("SHUTTLE_MINI_APP_LAUNCH_URL", tc.launch)
			allow := tc.allow
			if allow == "" {
				allow = "false"
			}
			t.Setenv("SHUTTLE_MINI_APP_ALLOW_LOCAL_HTTP", allow)
			c, err := config.Load(configFile(t, ""))
			if !tc.valid {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, c.MiniAppURL())
			require.Equal(t, tc.origin, c.MiniAppOrigin)
		})
	}
}

func TestMiniAppLocalHTTPEnvironmentFalseOverridesYAML(t *testing.T) {
	path := configFile(t, "")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	value := strings.Replace(string(data), `mini_app_origin: ""`, `mini_app_origin: "http://localhost:18382"`, 1)
	value += "\nmini_app_launch_url: \"https://complynx.net/testbot/18382/app/\"\nmini_app_allow_local_http: true\n"
	require.NoError(t, os.WriteFile(path, []byte(value), 0600))
	c, err := config.Load(path)
	require.NoError(t, err)
	require.True(t, c.MiniAppAllowLocalHTTP)
	t.Setenv("SHUTTLE_MINI_APP_ALLOW_LOCAL_HTTP", "false")
	_, err = config.Load(path)
	require.Error(t, err)
}
