package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestPublicSubtreeRedirects(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{Telegram: config.Telegram{WebAppURL: "https://example.test" + prefix + "/miniapp/"}}
			service := &browserauth.Service{Origin: "https://example.test"}
			mux := appMux(&bot.Bot{BrowserAuth: service}, cfg)
			mux.Handle(
				"/",
				api.Handler(
					runtimeapp.NewServices(nil, runtimeapp.Options{}),
					identity.Signer{},
					slog.New(slog.DiscardHandler),
				),
			)
			proxy := httptest.NewServer(http.StripPrefix(prefix, publicHandler(mux, cfg)))
			t.Cleanup(proxy.Close)
			client := proxy.Client()
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
				for _, route := range []string{
					"/miniapp/api", "/miniapp/%61pi", "/miniapp/auth", "/miniapp/%61uth",
					"/v1", "/%761", "/internal", "/%69nternal", "/internal/food", "/internal/%66ood",
					"/internal/admin-messages", "/internal/%61dmin-messages",
				} {
					const query = "?id=a%26x%3D1&foo=a%23b&same=1&same=2"
					request, err := http.NewRequestWithContext(t.Context(), method, proxy.URL+prefix+route+query, nil)
					require.NoError(t, err)
					request.Host = "spoofed.example"
					request.Header.Set("X-Forwarded-Prefix", "/evil")
					request.Header.Set("X-Forwarded-Host", "evil.example")
					response, err := client.Do(request)
					require.NoError(t, err)
					body, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode, method+" "+route)
					require.Equal(t, prefix+route+"/"+query, response.Header.Get("Location"), method+" "+route)
					if method == http.MethodGet {
						require.Contains(t, string(body), `href="`+prefix+route+"/", route)
					}
				}
			}
		})
	}
}
