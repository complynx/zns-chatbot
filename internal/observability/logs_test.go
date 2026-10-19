package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestContextualLogsIncludeOnlyValidIDs(t *testing.T) {
	t.Parallel()
	state, err := trace.ParseTraceState("vendor=private-state")
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		flags trace.TraceFlags
		valid bool
	}{
		{"sampled", trace.FlagsSampled, true}, {"unsampled", 0, true}, {"invalid", trace.FlagsSampled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			logger := slog.New(observability.WithTraceContext(slog.NewJSONHandler(&output, nil)))
			config := trace.SpanContextConfig{SpanID: trace.SpanID{2}, TraceFlags: tc.flags, TraceState: state}
			if tc.valid {
				config.TraceID = trace.TraceID{1}
			}
			ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(config))
			logger.DebugContext(ctx, "filtered")
			logger.InfoContext(ctx, "diagnostic", "class", "timeout")
			require.NotContains(t, output.String(), "private-state")
			require.NotContains(t, output.String(), "filtered")
			var record map[string]any
			require.NoError(t, json.Unmarshal(output.Bytes(), &record))
			require.Equal(t, "timeout", record["class"])
			if tc.valid {
				require.Equal(t, "01000000000000000000000000000000", record["trace_id"])
				require.Equal(t, "0200000000000000", record["span_id"])
			} else {
				require.NotContains(t, record, "trace_id")
				require.NotContains(t, record, "span_id")
			}
		})
	}
}

func TestDerivedLoggersPreserveGroupsAndContextIsolation(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := slog.New(observability.WithTraceContext(slog.NewJSONHandler(&output, nil)))
	grouped := logger.With("deployment", "zinusya").WithGroup("task").With("kind", "read")
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	grouped.InfoContext(trace.ContextWithSpanContext(context.Background(), span), "grouped", "status", "ok")
	logger.InfoContext(context.Background(), "plain")
	decoder := json.NewDecoder(&output)
	var first, second map[string]any
	require.NoError(t, decoder.Decode(&first))
	require.Equal(t, "zinusya", first["deployment"])
	require.Equal(t, map[string]any{
		"kind": "read", "status": "ok", "trace_id": span.TraceID().String(), "span_id": span.SpanID().String(),
	}, first["task"])
	require.NotContains(t, first, "trace_id")
	require.NoError(t, decoder.Decode(&second))
	for _, key := range []string{"deployment", "task", "trace_id", "span_id"} {
		require.NotContains(t, second, key)
	}
}

func TestConcurrentContextualLogsKeepTheirOwnIDs(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := slog.New(observability.WithTraceContext(slog.NewJSONHandler(&output, nil))).With("class", "test")
	var writers sync.WaitGroup
	for n := byte(1); n <= 16; n++ {
		writers.Go(func() {
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{n}, SpanID: trace.SpanID{n}})
			logger.InfoContext(trace.ContextWithSpanContext(context.Background(), span), "concurrent", "number", int(n))
		})
	}
	writers.Wait()
	decoder := json.NewDecoder(&output)
	seen := map[int]bool{}
	for range 16 {
		var record map[string]any
		require.NoError(t, decoder.Decode(&record))
		number, ok := record["number"].(float64)
		require.True(t, ok)
		n := int(number)
		require.False(t, seen[n])
		seen[n] = true
		require.Equal(t, fmt.Sprintf("%02x000000000000000000000000000000", n), record["trace_id"])
		require.Equal(t, fmt.Sprintf("%02x00000000000000", n), record["span_id"])
	}
}

func TestHTTPDiagnosticLogUsesTheCurrentServerSpan(t *testing.T) {
	t.Parallel()
	telemetry, err := observability.New(context.Background(), observability.Options{SampleRatio: 1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = telemetry.Shutdown(context.Background()) })
	var output bytes.Buffer
	logger := slog.New(observability.WithTraceContext(slog.NewJSONHandler(&output, nil)))
	var current trace.SpanContext
	handler := telemetry.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current = trace.SpanContextFromContext(r.Context())
		logger.WarnContext(r.Context(), "dependency unavailable", "class", "timeout")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	request := httptest.NewRequest(http.MethodPost, "/private-token", nil)
	request.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	request.Header.Set("Tracestate", "vendor=private-state")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	var record map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &record))
	require.True(t, current.IsValid())
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", record["trace_id"])
	require.Equal(t, current.SpanID().String(), record["span_id"])
	require.NotEqual(t, "00f067aa0ba902b7", record["span_id"])
	require.NotContains(t, output.String(), "private-token")
	require.NotContains(t, output.String(), "private-state")
}
