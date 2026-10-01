package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestTelemetryClientFailureObservation(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	seen := make(chan http.Header, 5)
	server := httptest.NewServer(
		telemetryHandler(runtime, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status, parseErr := strconv.Atoi(r.Header.Get("X-Test-Status"))
			if parseErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			seen <- r.Header.Clone()
			w.Header().Set("X-Private-Result", "private-result")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, "private-body")
		})),
	)
	defer server.Close()
	span := trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled},
	)
	ctx := trace.ContextWithSpanContext(t.Context(), span)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusConflict, http.StatusServiceUnavailable} {
		// The actual factory may construct several adapters sharing one registry.
		client := telemetryClient(runtime, "api", time.Second)
		request, requestErr := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			server.URL+"/private-path?token=private-query",
			nil,
		)
		require.NoError(t, requestErr)
		request.Header.Set("X-Test-Status", strconv.Itoa(status))
		request.Header.Set("Authorization", "Bearer private-credential")
		request.Header.Set("Baggage", "subject=private-subject")
		response, callErr := client.Do(request)
		require.NoError(t, callErr)
		body, readErr := io.ReadAll(response.Body)
		require.NoError(t, readErr)
		require.NoError(t, response.Body.Close())
		require.Equal(t, status, response.StatusCode)
		require.Equal(t, "private-body", string(body))
		require.Equal(t, "private-result", response.Header.Get("X-Private-Result"))
		headers := <-seen
		require.Empty(t, headers.Get("Baggage"))
		require.NotEmpty(t, headers.Get("Traceparent"))
		require.Equal(t, request.Header.Get("Authorization"), headers.Get("Authorization"))
		require.Equal(t, "subject=private-subject", request.Header.Get("Baggage"))
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	request, err := http.NewRequestWithContext(canceled, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = telemetryClient(runtime, "api", time.Second).Do(request)
	require.ErrorIs(t, err, context.Canceled)
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := metrics.Body.String()
	for status, code := range map[int]string{
		http.StatusUnauthorized: "authorization_denied", http.StatusForbidden: "authorization_denied",
		http.StatusTooManyRequests: "rate_limited", http.StatusConflict: "request_rejected", http.StatusServiceUnavailable: "remote_failure",
	} {
		require.Contains(
			t,
			text,
			`code="`+code+`",operation="api",phase="response",retryability="unknown",transport_status="`+strconv.Itoa(
				status,
			)+`"} 1`,
		)
	}
	require.Contains(
		t,
		text,
		`code="canceled",operation="api",phase="roundtrip",retryability="unknown",transport_status="0"} 1`,
	)
	require.Contains(t, text, `zns_operations_total{operation="api",result="ok"} 4`)
	require.Contains(t, text, `zns_operations_total{operation="api",result="error"} 1`)
	require.Contains(t, text, `zns_operations_total{operation="api",result="canceled"} 1`)
	require.NotContains(t, text, "private-")
}

func TestTelemetryClientWithoutRuntimeUsesDefaultTransport(t *testing.T) {
	t.Parallel()
	seen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("X-Private", "private-header")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "private-body")
	}))
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/private-path", nil)
	require.NoError(t, err)
	request.Header.Set("Traceparent", "private-existing-trace")
	request.Header.Set("Authorization", "private-credential")
	request.Header.Set("Baggage", "private-baggage")
	response, err := telemetryClient(nil, "api", time.Second).Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, response.StatusCode)
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "private-body", string(data))
	require.Equal(t, "private-header", response.Header.Get("X-Private"))
	headers := <-seen
	for _, name := range []string{"Traceparent", "Authorization", "Baggage"} {
		require.Equal(t, request.Header.Get(name), headers.Get(name))
	}
}
