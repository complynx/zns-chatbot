package observability_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestDisabledMetricsAndCardinality(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	for index := range 100 {
		_, finish := runtime.Start(t.Context(), "/users/"+strconv.Itoa(index)+"?secret=yes")
		finish(errors.New("password-secret"))
		handler := runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest("CUSTOM"+strconv.Itoa(index), "/private/"+strconv.Itoa(index), nil),
		)
	}
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := response.Body.String()
	assert.Contains(t, text, `zns_operations_total{operation="unknown",result="error"} 100`)
	assert.Contains(t, text, `zns_operations_total{operation="server",result="ok"} 100`)
	assert.NotContains(t, text, "secret")
	assert.NotContains(t, text, "CUSTOM")
	assert.NotContains(t, text, "/private/")
	families, err := runtime.Registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		assert.Len(t, family.GetMetric(), 2)
	}
	require.NoError(t, runtime.Shutdown(t.Context()))
}

type spanSink struct {
	mu    sync.Mutex
	spans []*tracepb.Span
}

func (s *spanSink) serve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var request collector.ExportTraceServiceRequest
	if err = proto.Unmarshal(body, &request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, resource := range request.GetResourceSpans() {
		for _, scope := range resource.GetScopeSpans() {
			s.spans = append(s.spans, scope.GetSpans()...)
		}
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func TestOTLPDeliveryParentCorrelationAndPrivacy(t *testing.T) {
	t.Parallel()
	var sink spanSink
	collectorServer := httptest.NewServer(http.HandlerFunc(sink.serve))
	defer collectorServer.Close()
	runtime, err := observability.New(
		t.Context(),
		observability.Config{Enabled: true, Endpoint: collectorServer.URL, SampleRatio: 1},
	)
	require.NoError(t, err)
	var output bytes.Buffer
	logger := observability.NewLogger(&output, observability.LogConfig{})
	var receivedBaggage string
	server := httptest.NewServer(
		runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedBaggage = r.Header.Get("Baggage")
			ctx, finish := runtime.Start(r.Context(), "db")
			logger.InfoContext(ctx, "query complete")
			finish(errors.New("postgres://user:secret@database/private"))
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	defer server.Close()
	ctx, finish := runtime.Start(t.Context(), "telegram.update")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/user/secret?token=private", nil)
	require.NoError(t, err)
	request.Header.Set("Baggage", "user=private")
	client := &http.Client{Transport: runtime.HTTPClient("api", http.DefaultTransport), Timeout: time.Second}
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	assert.Empty(t, receivedBaggage)
	assert.Equal(t, "user=private", request.Header.Get("Baggage"))
	assert.Empty(t, request.Header.Get("Traceparent"))
	finish(nil)
	shutdownCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(shutdownCtx))
	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.Len(t, sink.spans, 4)
	byName := map[string]*tracepb.Span{}
	for _, span := range sink.spans {
		byName[span.GetName()] = span
		assert.NotContains(t, span.String(), "secret")
		assert.Empty(t, span.GetAttributes())
	}
	assert.Equal(t, byName["telegram.update"].GetSpanId(), byName["api"].GetParentSpanId())
	assert.Equal(t, byName["api"].GetSpanId(), byName["server"].GetParentSpanId())
	assert.Equal(t, byName["server"].GetSpanId(), byName["db"].GetParentSpanId())
	assert.Contains(t, output.String(), `"trace_id"`)
	assert.Contains(t, output.String(), `"span_id"`)
	assert.Contains(t, output.String(), hex.EncodeToString(byName["db"].GetTraceId()))
	assert.Contains(t, output.String(), hex.EncodeToString(byName["db"].GetSpanId()))
	assert.NotContains(t, output.String(), "secret")
}

func TestInvalidTelemetryConfigDoesNotExposeEndpoint(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"http://user:secret@localhost", "http://localhost?token=secret", "invalid-secret"} {
		_, err := observability.New(
			t.Context(),
			observability.Config{Enabled: true, Endpoint: endpoint, SampleRatio: 1},
		)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
	}
}
