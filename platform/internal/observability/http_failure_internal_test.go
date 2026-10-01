package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

type failureObservationTransport func(*http.Request) (*http.Response, error)

func (f failureObservationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type opaqueTransportError struct{ timeout bool }

func (*opaqueTransportError) Error() string   { panic("private error text must not be read") }
func (e *opaqueTransportError) Timeout() bool { return e.timeout }
func (*opaqueTransportError) Temporary() bool { return false }

func TestHTTPFailureClassificationUsesOnlySafeSignals(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		status                   int
		err                      error
		code, phase, statusLabel string
		exists                   bool
	}{
		{status: 200}, {status: 302},
		{status: 401, code: "authorization_denied", phase: "response", statusLabel: "401", exists: true},
		{status: 403, code: "authorization_denied", phase: "response", statusLabel: "403", exists: true},
		{status: 429, code: "rate_limited", phase: "response", statusLabel: "429", exists: true},
		{status: 409, code: "request_rejected", phase: "response", statusLabel: "409", exists: true},
		{status: 503, code: "remote_failure", phase: "response", statusLabel: "503", exists: true},
		{status: 700, code: "invalid_transport_response", phase: "roundtrip", statusLabel: "0", exists: true},
		{err: errors.Join(context.Canceled, &opaqueTransportError{}), code: "canceled", phase: "roundtrip", statusLabel: "0", exists: true},
		{err: errors.Join(context.DeadlineExceeded, &opaqueTransportError{}), code: "timeout", phase: "roundtrip", statusLabel: "0", exists: true},
		{err: &opaqueTransportError{timeout: true}, code: "timeout", phase: "roundtrip", statusLabel: "0", exists: true},
		{status: 403, err: &opaqueTransportError{}, code: "transport_unavailable", phase: "roundtrip", statusLabel: "403", exists: true},
	} {
		var response *http.Response
		if item.status != 0 {
			response = &http.Response{StatusCode: item.status}
		}
		value, exists := classifyHTTPFailure(response, item.err)
		require.Equal(t, item.exists, exists)
		if exists {
			require.Equal(t, httpFailure{phase: item.phase, code: item.code, status: item.statusLabel}, value)
		}
	}
	value, exists := classifyHTTPFailure(nil, nil)
	require.True(t, exists)
	require.Equal(t, httpFailure{phase: "roundtrip", code: "invalid_transport_response", status: "0"}, value)
}

func TestHTTPFailureObservationCompatibleLabelOrderAndConflict(t *testing.T) {
	t.Parallel()
	registry := prometheus.NewRegistry()
	// Registration permits a compatible label set with a different declaration order.
	compatible := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: httpFailureMetricName,
		Help: httpFailureMetricHelp,
	}, []string{"phase", "operation", "transport_status", "code", "retryability"})
	require.NoError(t, registry.Register(compatible))
	actual := registerHTTPFailures(registry)
	require.Same(t, compatible, actual)
	observeHTTPFailure(actual, "api", &http.Response{StatusCode: http.StatusTooManyRequests}, nil)
	response := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Contains(
		t,
		response.Body.String(),
		`code="rate_limited",operation="api",phase="response",retryability="unknown",transport_status="429"`,
	)
	conflict := prometheus.NewRegistry()
	require.NoError(t, conflict.Register(prometheus.NewGauge(prometheus.GaugeOpts{
		Name: httpFailureMetricName, Help: "conflicting observation descriptor",
	})))
	require.Nil(t, registerHTTPFailures(conflict), "observation conflict cannot become a business failure")
}

func TestHTTPFailureObservationPreservesTransportAndRedaction(t *testing.T) {
	t.Parallel()
	runtime, err := New(t.Context(), Config{})
	require.NoError(t, err)
	span := trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled},
	)
	ctx := trace.ContextWithSpanContext(t.Context(), span)
	request := httptest.NewRequest(http.MethodPost, "https://private-host/private-path?token=private-token", nil).
		WithContext(ctx)
	request.Header.Set("Baggage", "private-subject")
	request.Header.Set("Authorization", "Bearer private-token")
	for _, status := range []int{401, 403, 429, 503} {
		body := io.NopCloser(bytes.NewBufferString("private-response"))
		response := &http.Response{
			StatusCode: status,
			Header:     http.Header{"X-Private": []string{"private-header"}},
			Body:       body,
		}
		wrapped := runtime.HTTPClient(
			"api",
			failureObservationTransport(func(received *http.Request) (*http.Response, error) {
				require.NotSame(t, request, received)
				require.Empty(t, received.Header.Get("Baggage"))
				require.Equal(t, "Bearer private-token", received.Header.Get("Authorization"))
				require.NotEmpty(t, received.Header.Get("Traceparent"))
				return response, nil
			}),
		)
		actual, callErr := wrapped.RoundTrip(request)
		require.NoError(t, callErr)
		require.Same(t, response, actual)
		require.True(t, body == actual.Body, "the original body interface is returned unchanged")
		data, readErr := io.ReadAll(actual.Body)
		require.NoError(t, readErr)
		require.Equal(t, "private-response", string(data))
		require.NoError(t, actual.Body.Close())
		require.Equal(t, "private-header", actual.Header.Get("X-Private"))
	}
	privateErr := &opaqueTransportError{}
	wrapped := runtime.HTTPClient(
		"api",
		failureObservationTransport(func(*http.Request) (*http.Response, error) { return nil, privateErr }),
	)
	response, callErr := wrapped.RoundTrip(request)
	require.Nil(t, response)
	require.Same(t, privateErr, callErr, "the original transport error must be returned unchanged")
	var logs bytes.Buffer
	NewLogger(&logs, LogConfig{}).ErrorContext(ctx, "request failed", slog.Any("error", callErr))
	require.Contains(t, logs.String(), "operation failed")
	require.NotContains(t, logs.String(), "private")
	require.Equal(t, "private-subject", request.Header.Get("Baggage"), "original headers are untouched")
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := metrics.Body.String()
	for _, code := range []string{"authorization_denied", "rate_limited", "remote_failure", "transport_unavailable"} {
		require.Contains(t, text, `code="`+code+`"`)
	}
	require.Contains(t, text, `retryability="unknown"`)
	require.Contains(t, text, `transport_status="429"`)
	require.Contains(
		t,
		text,
		`zns_operations_total{operation="api",result="ok"} 3`,
		"existing result semantics remain unchanged",
	)
	require.Contains(t, text, `zns_operations_total{operation="api",result="error"} 2`)
	for _, private := range []string{"private", "Authorization", "Bearer", "http://", "https://"} {
		require.NotContains(t, text, private)
	}
}

func TestHTTPFailureObservationRegistryIsolationAndBounds(t *testing.T) {
	t.Parallel()
	first := prometheus.NewRegistry()
	second := prometheus.NewRegistry()
	counter := registerHTTPFailures(first)
	require.Same(t, counter, registerHTTPFailures(first), "multiple HTTP clients share one collector")
	other := registerHTTPFailures(second)
	require.NotSame(t, counter, other)
	for status := -10; status <= 700; status++ {
		observeHTTPFailure(counter, "private-user-operation", &http.Response{StatusCode: status}, nil)
	}
	families, err := first.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	require.Len(t, families[0].GetMetric(), 201, "only200 actual failure status codes plus unavailable bucket")
	for _, metric := range families[0].GetMetric() {
		for _, label := range metric.GetLabel() {
			if label.GetName() == "operation" {
				require.Equal(t, "unknown", label.GetValue())
			}
			if label.GetName() == "retryability" {
				require.Equal(t, "unknown", label.GetValue())
			}
		}
	}
	empty, err := second.Gather()
	require.NoError(t, err)
	require.Empty(t, empty)
}
