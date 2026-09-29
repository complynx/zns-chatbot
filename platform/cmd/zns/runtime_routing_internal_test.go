package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/browserauth"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func runtimeRoutingServer(t *testing.T, prefix string) string {
	t.Helper()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	cfg := config.Config{
		Telegram: config.Telegram{WebAppURL: "https://example.test" + prefix + "/miniapp/"},
		Shutdown: config.Shutdown{Drain: time.Second},
	}
	mux := appMux(&bot.Bot{BrowserAuth: &browserauth.Service{Origin: "https://example.test"}}, cfg)
	mux.Handle(
		"/",
		api.Handler(
			runtimeapp.NewServices(nil, runtimeapp.Options{}),
			identity.Signer{},
			slog.New(slog.DiscardHandler),
		),
	)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/probe/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Route-Value", r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /leave", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://outside.example/login?x=1#part", http.StatusTemporaryRedirect)
	})
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() {
		finished <- serveListener(ctx, listener, telemetryHandler(runtime, publicHandler(mux, cfg)), slog.New(slog.DiscardHandler), cfg)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-finished)
	})
	return "http://" + listener.Addr().String()
}

func TestRuntimePublicRouting(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"", "/bot"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			direct := runtimeRoutingServer(t, prefix)
			target, err := url.Parse(direct)
			require.NoError(t, err)
			proxy := httptest.NewServer(http.StripPrefix(prefix, httputil.NewSingleHostReverseProxy(target)))
			t.Cleanup(proxy.Close)
			client := &http.Client{
				Timeout:       5 * time.Second,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
			}
			checkRuntimeMalformed(t, client, direct, proxy.URL+prefix)
			checkRuntimeSubtrees(t, client, proxy.URL+prefix, prefix)
			checkRuntimeMetrics(t, client, proxy.URL+prefix)
			checkRuntimeControls(t, client, proxy.URL+prefix)
		})
	}
}

func checkRuntimeMalformed(t *testing.T, client *http.Client, direct, base string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, route := range []string{"//miniapp/../v1", "/miniapp/../outside", "/miniapp//api", "/%2fminiapp/../outside", "/v1//probe/value", "/metrics/../v1", "//", "/miniapp/auth/%2e/check"} {
			for _, address := range []string{direct, base} {
				request, requestErr := http.NewRequestWithContext(
					t.Context(),
					method,
					address+route+"?x=a%26b&x=2",
					nil,
				)
				require.NoError(t, requestErr)
				response, responseErr := client.Do(request)
				require.NoError(t, responseErr)
				require.NoError(t, response.Body.Close())
				t.Logf(
					"%s %s => %d Location=%q",
					method,
					address+route,
					response.StatusCode,
					response.Header.Get("Location"),
				)
				assert.Equal(t, http.StatusNotFound, response.StatusCode)
				assert.Empty(t, response.Header.Get("Location"))
			}
		}
	}
}

func checkRuntimeSubtrees(t *testing.T, client *http.Client, base, prefix string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		for _, route := range []string{"/miniapp/api", "/miniapp/%61pi", "/miniapp/auth", "/miniapp/%61uth", "/v1", "/%761", "/internal", "/%69nternal", "/internal/food", "/internal/%66ood", "/internal/admin-messages", "/internal/%61dmin-messages"} {
			request, requestErr := http.NewRequestWithContext(t.Context(), method, base+route+"?x=a%26b&x=2", nil)
			require.NoError(t, requestErr)
			request.Header.Set("X-Forwarded-Prefix", "/evil")
			response, responseErr := client.Do(request)
			require.NoError(t, responseErr)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
			assert.Equal(t, prefix+route+"/?x=a%26b&x=2", response.Header.Get("Location"))
		}
	}
}

func checkRuntimeMetrics(t *testing.T, client *http.Client, base string) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		metricsRequest, metricsErr := http.NewRequestWithContext(t.Context(), method, base+"/metrics", nil)
		require.NoError(t, metricsErr)
		metricsRequest.Header.Set("Authorization", "Bearer invalid-test-token")
		metricsResponse, metricsErr := client.Do(metricsRequest)
		require.NoError(t, metricsErr)
		require.NoError(t, metricsResponse.Body.Close())
		assert.Equal(t, http.StatusOK, metricsResponse.StatusCode)
	}
}

func checkRuntimeControls(t *testing.T, client *http.Client, base string) {
	t.Helper()
	for _, route := range []string{"/", "/miniapp/", "/v1/probe/a%2F..%2Fb", "/leave", "/metrics", "/%6detrics", "/v1/catalog"} {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, base+route, nil)
		require.NoError(t, requestErr)
		response, responseErr := client.Do(request)
		require.NoError(t, responseErr)
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, readErr)
		require.NoError(t, response.Body.Close())
		switch route {
		case "/", "/v1/probe/a%2F..%2Fb":
			assert.Equal(t, http.StatusNoContent, response.StatusCode)
			if route != "/" {
				assert.Equal(t, "a/../b", response.Header.Get("X-Route-Value"))
			}
		case "/leave":
			assert.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
			assert.Equal(t, "https://outside.example/login?x=1#part", response.Header.Get("Location"))
		case "/miniapp/":
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, string(body), "order-form")
		case "/v1/catalog":
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		default:
			assert.Equal(t, http.StatusOK, response.StatusCode)
			assert.Contains(t, string(body), "zns_operations_total")
		}
	}
}
