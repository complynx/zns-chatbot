package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestPublicNoncanonicalPaths(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			proxy := publicRoutingServer(t, prefix)
			client := proxy.Client()
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
				for _, route := range []string{
					"/miniapp//", "//miniapp", "/miniapp/./", "/miniapp/../menu", "/x/../miniapp",
					"//menu", "/menu/../v1", "/massage_timetable/./", "/auth//", "/miniapp/auth/%2e/check",
					"//miniapp/../v1", "//menu/../outside", "//auth/../outside", "//massage_timetable/../outside",
					"/%2fminiapp/../outside", "/%2Fmenu/../outside", "/v1/../outside", "/v1//probe/value",
					"//", "///", "/./", "/../",
				} {
					request, err := http.NewRequestWithContext(
						t.Context(),
						method,
						proxy.URL+prefix+route+"?id=a%26x%3D1",
						nil,
					)
					require.NoError(t, err)
					request.Header.Set("X-Forwarded-Prefix", "/evil")
					response, err := client.Do(request)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					require.Equal(t, http.StatusNotFound, response.StatusCode, method+" "+prefix+route)
					require.Empty(t, response.Header.Get("Location"), method+" "+prefix+route)
				}
			}
		})
	}
}

func publicRoutingServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	cfg := config.Config{Telegram: config.Telegram{WebAppURL: "https://example.test" + prefix + "/miniapp/"}}
	mux := appMux(&bot.Bot{}, cfg)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/probe/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Route-Value", r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	proxy := httptest.NewServer(http.StripPrefix(prefix, publicHandler(mux, cfg)))
	t.Cleanup(proxy.Close)
	return proxy
}

func TestPublicCanonicalRoutingControls(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			proxy := publicRoutingServer(t, prefix)
			client := proxy.Client()
			client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
			for _, test := range []struct {
				method, route string
				status        int
			}{
				{http.MethodGet, "/", http.StatusOK},
				{http.MethodHead, "/", http.StatusOK},
				{http.MethodGet, "/miniapp", http.StatusTemporaryRedirect},
				{http.MethodHead, "/miniapp", http.StatusTemporaryRedirect},
				{http.MethodPost, "/miniapp", http.StatusMethodNotAllowed},
				{http.MethodGet, "/miniapp/editor%2Ejs", http.StatusOK},
				{http.MethodGet, "/miniapp%2f", http.StatusNotFound},
				{http.MethodGet, "/miniapp/%2e/", http.StatusNotFound},
				{http.MethodGet, "/%2e/miniapp", http.StatusNotFound},
				{http.MethodGet, "/miniapp/%2e%2e/menu", http.StatusNotFound},
				{http.MethodGet, "/miniapp/api/orders/a%2F..%2Fb", http.StatusUnauthorized},
				{http.MethodGet, "/miniapp/api/orders/%2E", http.StatusUnauthorized},
				{http.MethodGet, "/v1/probe/a%2F..%2Fb", http.StatusNoContent},
				{http.MethodGet, "/v1//probe/value", http.StatusNotFound},
				{http.MethodHead, "/miniapp/", http.StatusOK},
				{http.MethodPost, "/miniapp/", http.StatusMethodNotAllowed},
				{http.MethodGet, "/menu", http.StatusOK},
				{http.MethodGet, "/massage_timetable", http.StatusOK},
			} {
				request, err := http.NewRequestWithContext(
					t.Context(),
					test.method,
					proxy.URL+prefix+test.route+"?id=a%26x%3D1",
					nil,
				)
				require.NoError(t, err)
				response, err := client.Do(request)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.Equal(t, test.status, response.StatusCode, test.method+" "+prefix+test.route)
				if test.status == http.StatusTemporaryRedirect {
					require.Equal(t, prefix+"/miniapp/?id=a%26x%3D1", response.Header.Get("Location"))
				} else {
					require.Empty(t, response.Header.Get("Location"))
				}
				if test.status == http.StatusNoContent {
					require.Equal(t, "a/../b", response.Header.Get("X-Route-Value"))
				}
			}
		})
	}
}

func TestPublicInvalidPathEncoding(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		proxy := publicRoutingServer(t, prefix)
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(
			t.Context(),
			"tcp",
			proxy.Listener.Addr().String(),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = connection.Close() })
		require.NoError(t, connection.SetDeadline(time.Now().Add(5*time.Second)))
		_, err = io.WriteString(
			connection,
			"GET "+prefix+"/miniapp/%zz HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n",
		)
		require.NoError(t, err)
		response, err := http.ReadResponse(bufio.NewReader(connection), nil)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		require.Empty(t, response.Header.Get("Location"))
	}
}

func TestPublicEncodedMountSeparator(t *testing.T) {
	t.Parallel()
	proxy := publicRoutingServer(t, "/bot")
	client := proxy.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, route := range []string{"/bot%2fminiapp", "/bot%2Fmenu", "/bot%2fauth", "/bot%2fmassage_timetable"} {
			request, err := http.NewRequestWithContext(t.Context(), method, proxy.URL+route+"?id=a%26x%3D1", nil)
			require.NoError(t, err)
			response, err := client.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusNotFound, response.StatusCode, method+" "+route)
			require.Empty(t, response.Header.Get("Location"))
		}
	}
}
