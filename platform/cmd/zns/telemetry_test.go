package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestConfiguredLoggerRegistersSecrets(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Log: config.Log{Level: "debug"}, Database: config.Database{URL: "db-secret"},
		Auth: config.Auth{SigningKey: "sign-secret"}, Telegram: config.Telegram{Token: "tg-secret"},
		Model: config.Model{OpenAIKey: "model-secret"}, Media: config.Worker{Secret: "media-secret"},
		Sticker: config.Sticker{Worker: config.Worker{Secret: "sticker-secret"}}}
	var output bytes.Buffer
	logger := configuredLogger(&output, cfg)
	logger.DebugContext(t.Context(), "db-secret sign-secret tg-secret model-secret media-secret sticker-secret")
	assert.NotContains(t, output.String(), "-secret")
	assert.Contains(t, output.String(), "DEBUG")
	logger.ErrorContext(t.Context(), "startup failed", "error", errors.New("postgres://user:password@host/private"))
	assert.NotContains(t, output.String(), "password")
}

func TestTelemetryHTTPWiring(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	server := httptest.NewServer(
		telemetryHandler(runtime, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	defer server.Close()
	client := telemetryClient(runtime, "api", time.Second)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/private?token=secret", nil)
	require.NoError(t, err)
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/metrics", nil)
	require.NoError(t, err)
	response, err = server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `zns_operations_total{operation="server",result="ok"} 1`)
	assert.Contains(t, string(body), `zns_operations_total{operation="api",result="ok"} 1`)
	assert.NotContains(t, string(body), "private")
	assert.NotContains(t, string(body), "secret")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, flushTelemetry(canceled, runtime, time.Second))
}

func TestModelProviderWiring(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	model, err := botModel(config.Config{Model: config.Model{Provider: "scripted"}}, runtime)
	require.NoError(t, err)
	_, err = model.Plan(t.Context(), agent.Input{Text: "hello"})
	require.NoError(t, err)
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, metrics.Body.String(), `zns_operations_total{operation="model.plan",result="ok"} 1`)
}
