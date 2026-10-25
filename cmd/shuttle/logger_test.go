package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestApplicationLoggerCorrelatesContextWithoutChangingPlainLogs(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := newLogger(&output)
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	logger.WarnContext(trace.ContextWithSpanContext(context.Background(), span), "diagnostic", "class", "timeout")
	logger.Info("ready")
	decoder := json.NewDecoder(&output)
	var correlated, plain map[string]any
	require.NoError(t, decoder.Decode(&correlated))
	require.Equal(t, "01000000000000000000000000000000", correlated["trace_id"])
	require.Equal(t, "0200000000000000", correlated["span_id"])
	require.Equal(t, "WARN", correlated["level"])
	require.Equal(t, "timeout", correlated["class"])
	require.NoError(t, decoder.Decode(&plain))
	require.Equal(t, "ready", plain["msg"])
	require.NotContains(t, plain, "trace_id")
	require.NotContains(t, plain, "span_id")
}

func TestApplicationDebugLoggingIsOptIn(t *testing.T) {
	for _, level := range []string{"", "debug"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("SHUTTLE_LOG_LEVEL", level)
			var output bytes.Buffer
			newLogger(&output).Debug("technical stage", "attempt", 1)
			require.Equal(t, level == "debug", output.Len() > 0)
		})
	}
}
