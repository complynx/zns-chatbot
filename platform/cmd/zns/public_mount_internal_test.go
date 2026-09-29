package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestAppPublicMountRedirect(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			cfg := config.Config{Telegram: config.Telegram{WebAppURL: "https://example.test" + prefix + "/miniapp/"}}
			proxy := httptest.NewServer(http.StripPrefix(prefix, appMux(&bot.Bot{}, cfg)))
			t.Cleanup(proxy.Close)
			client := proxy.Client()
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			const query = "?order_id=a%26x%3D1&foo=a%23b"
			request, err := http.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				proxy.URL+prefix+"/miniapp"+query,
				nil,
			)
			require.NoError(t, err)
			request.Header.Set("X-Forwarded-Prefix", "/evil")
			request.Header.Set("Forwarded", "host=evil.test;proto=https")
			response, err := client.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
			require.Equal(t, prefix+"/miniapp/"+query, response.Header.Get("Location"))
			request, err = http.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				proxy.URL+response.Header.Get("Location"),
				nil,
			)
			require.NoError(t, err)
			page, err := client.Do(request)
			require.NoError(t, err)
			defer page.Body.Close()
			require.Equal(t, http.StatusOK, page.StatusCode)
		})
	}
}
