package miniapp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/miniapp"
)

func TestMiniAppOriginIsAnExplicitHTTPSOrigin(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"https://app.example.invalid", "https://localhost:8443", "https://[::1]:8443"} {
		require.True(t, miniapp.ValidOrigin(valid), valid)
	}
	for _, invalid := range []string{"", "*", "null", "http://localhost:8443", "https://app.example.invalid/",
		"https://APP.example.invalid", "https://app.example.invalid:443",
		"https://app.example.invalid/path", "https://app.example.invalid?", "https://app.example.invalid#",
		"https://user:password@app.example.invalid", "https://app.example.invalid?x=1",
		"https://app.example.invalid#fragment",
	} {
		require.False(t, miniapp.ValidOrigin(invalid), invalid)
	}
}

func TestMiniAppLocalSessionOriginRequiresOptIn(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"http://localhost", "http://localhost:18382"} {
		require.False(t, miniapp.ValidSessionOrigin(origin, false), origin)
		require.True(t, miniapp.ValidSessionOrigin(origin, true), origin)
	}
	for _, origin := range []string{
		"http://example.invalid",
		"http://localhost.evil.invalid:18382",
		"http://abc.localhost:18382",
		"http://127.0.0.1:18382",
		"http://[::1]:18382",
		"http://LOCALHOST:18382",
		"http://localhost:80",
		"http://localhost:0",
		"http://localhost:65536",
		"http://localhost:018382",
		"http://localhost:",
		"http://user:secret@localhost:18382",
		"http://localhost:18382/",
		"http://localhost:18382?x=1",
	} {
		require.False(t, miniapp.ValidSessionOrigin(origin, true), origin)
	}
	require.True(t, miniapp.ValidSessionOrigin("https://app.example.invalid", false))
}

func TestMiniAppLaunchURLAllowsCanonicalHTTPSRedirectPrefix(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"https://app.example.invalid/app/",
		"https://complynx.net/testbot/18382/app/",
		"https://app.example.invalid:8443/app/",
	} {
		require.True(t, miniapp.ValidLaunchURL(value), value)
	}
	for _, value := range []string{
		"",
		"http://localhost:18382/app/",
		"https://user:secret@example.invalid/app/",
		"https://example.invalid/app/?token=secret",
		"https://example.invalid/app/#orders",
		"https://example.invalid:443/app/",
		"https://example.invalid:65536/app/",
		"https://example.invalid:00081/app/",
		"https://example.invalid:/app/",
		"https://EXAMPLE.invalid/app/",
		"https://example.invalid/prefix/../app/",
		"https://example.invalid//app/",
		"https://example.invalid/%61pp/",
		"https://example.invalid/app",
	} {
		require.False(t, miniapp.ValidLaunchURL(value), value)
	}
}
