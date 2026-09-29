package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeEscapedMiniAppEntry(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			target, err := url.Parse(runtimeRoutingServer(t, prefix))
			require.NoError(t, err)
			proxy := httptest.NewServer(http.StripPrefix(prefix, httputil.NewSingleHostReverseProxy(target)))
			t.Cleanup(proxy.Close)
			client := proxy.Client()
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			for _, route := range []string{"/miniapp", "/%6diniapp", "/mini%61pp", "/%6Diniapp"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					const query = "?order_id=a%2Fb&same=1&same=2&plus=a+b"
					request, requestErr := http.NewRequestWithContext(
						t.Context(), method, proxy.URL+prefix+route+query, nil,
					)
					require.NoError(t, requestErr)
					request.Host = "untrusted.example"
					request.Header.Set("X-Forwarded-Prefix", "/evil")
					response, responseErr := client.Do(request)
					require.NoError(t, responseErr)
					body, readErr := io.ReadAll(response.Body)
					require.NoError(t, readErr)
					require.NoError(t, response.Body.Close())
					if method == http.MethodPost {
						require.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
						require.Empty(t, response.Header.Get("Location"))
						continue
					}
					require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
					require.Equal(t, prefix+route+"/"+query, response.Header.Get("Location"))
					if method == http.MethodGet {
						require.Contains(t, string(body), `href="`+prefix+route+"/")
					}
				}
			}
		})
	}
}
