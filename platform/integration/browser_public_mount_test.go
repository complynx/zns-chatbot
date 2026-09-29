package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
)

func TestBrowserPublicMountConsent(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			f, _ := browserFixture(t)
			server := httptest.NewUnstartedServer(nil)
			origin := "http://" + server.Listener.Addr().String()
			publicURL := origin + prefix + "/miniapp/"
			service, err := browserauth.New(f.db, f.b.API.Signer, f.b.TG, publicURL,
				f.b.API.BrowserAuthRecipient, f.b.API.AuthenticateTelegram)
			require.NoError(t, err)
			f.b.BrowserAuth = service
			server.Config.Handler = http.StripPrefix(prefix, (miniapp.Gateway{
				WebAppURL: publicURL, BrowserAuth: service, API: f.b.API,
			}).Handler())
			server.Start()
			t.Cleanup(server.Close)
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			client := server.Client()
			client.Jar = jar
			request := func(method, route, body, requestOrigin string, status int) map[string]string {
				t.Helper()
				req, requestErr := http.NewRequestWithContext(
					t.Context(), method, publicURL+route, strings.NewReader(body),
				)
				require.NoError(t, requestErr)
				req.Header.Set("Origin", requestOrigin)
				req.Header.Set("X-Browser-Auth", "1")
				req.Header.Set("X-Forwarded-Prefix", "/evil")
				response, requestErr := client.Do(req)
				require.NoError(t, requestErr)
				defer response.Body.Close()
				require.Equal(t, status, response.StatusCode)
				for _, cookie := range response.Cookies() {
					require.Equal(t, prefix+"/miniapp", cookie.Path)
					require.True(t, cookie.HttpOnly)
					require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
				}
				var value map[string]string
				require.NoError(t, json.NewDecoder(response.Body).Decode(&value))
				return value
			}
			started := request(http.MethodPost, "auth/start", `{"username":"alice"}`, origin, http.StatusOK)
			authCallback(t, f, started["request"], "approve", 19001)
			approved := request(http.MethodGet, "auth/status?request="+started["request"], "", origin, http.StatusOK)
			require.Equal(t, "approved", approved["result"])
			request(http.MethodGet, "auth/check", "", origin, http.StatusOK)
			outside, err := url.Parse(origin + "/unrelated")
			require.NoError(t, err)
			require.Empty(t, jar.Cookies(outside))
			request(http.MethodPost, "auth/logout", `{}`, "https://evil.test", http.StatusForbidden)
			request(http.MethodGet, "auth/check", "", origin, http.StatusOK)
			request(http.MethodPost, "auth/logout", `{}`, origin, http.StatusOK)
			request(http.MethodGet, "auth/check", "", origin, http.StatusUnauthorized)
		})
	}
}
