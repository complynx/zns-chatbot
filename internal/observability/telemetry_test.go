//nolint:testpackage // In-memory spans verify scoped provider internals without exporting test hooks.
package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestMetricsBoundedAndPrivate(t *testing.T) {
	t.Parallel()
	globalTrace, globalMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	tm, err := New(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Shutdown(context.Background()) })
	canaries := []string{"private-user", "private-token"}
	for i := range 100 {
		component, outcome := fmt.Sprintf("user-%d-secret", i), fmt.Sprintf("token-%d", i)
		tm.Inc(component, outcome)
		canaries = append(canaries, component, outcome)
	}
	tm.Inc("queue", "accepted")
	h := tm.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }),
	)
	r := httptest.NewRequest(http.MethodPost, "/users/private-user?token=private-token", nil)
	h.ServeHTTP(httptest.NewRecorder(), r)
	w := httptest.NewRecorder()
	tm.MetricsHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := w.Body.String()
	for _, want := range []string{"go_goroutines", "process_cpu_seconds", "zinusya_operations_total", `component="other",outcome="other"} 100`, `component="queue",outcome="accepted"} 1`, "zinusya_http_requests_total"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing metrics %q in %s", want, body)
		}
	}
	for _, secret := range canaries {
		if strings.Contains(body, secret) {
			t.Errorf("metrics disclosed %q", secret)
		}
	}
	if otel.GetTracerProvider() != globalTrace || otel.GetMeterProvider() != globalMeter {
		t.Fatal("changed global providers")
	}
}

func TestIncomingTraceAndErrorAreSanitized(t *testing.T) {
	t.Parallel()
	tm, err := New(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Shutdown(context.Background()) })
	exp := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tm.tracer = provider.Tracer("test")
	h := tm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, end := tm.Start(r.Context(), "private-message")
		if !trace.SpanContextFromContext(ctx).IsValid() {
			t.Error("missing trace context")
		}
		end(errors.New("Bearer private-token message private-user"))
		end(nil)
		w.WriteHeader(http.StatusBadGateway)
	}))
	r := httptest.NewRequest(http.MethodPost, "/bot/private-token/users/private-user?secret=private-message", nil)
	r.Header.Set("Authorization", "Bearer private-token")
	r.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	r.Header.Set("Tracestate", "vendor=value")
	h.ServeHTTP(httptest.NewRecorder(), r)
	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	server := spans[1]
	if server.Parent.SpanID().String() != "00f067aa0ba902b7" ||
		server.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		server.SpanContext.TraceState().String() != "vendor=value" {
		t.Fatalf("lost incoming context: %+v", server)
	}
	if spans[0].Parent.SpanID() != server.SpanContext.SpanID() {
		t.Error("operation is not a child of HTTP span")
	}
	for _, span := range spans {
		payload := fmt.Sprintf("%s %v %v %v", span.Name, span.Attributes, span.Events, span.Status)
		if strings.Contains(payload, "private-") || strings.Contains(payload, "Bearer") {
			t.Errorf("span disclosed private data: %s", payload)
		}
	}
}

func TestExporterFailureDoesNotBlockRequest(t *testing.T) {
	t.Parallel()
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private-secret", http.StatusServiceUnavailable)
	}))
	defer collector.Close()
	tm, err := New(
		context.Background(),
		Options{OTLPEndpoint: collector.URL, SampleRatio: 1, Timeout: 25 * time.Millisecond, MaxQueueSize: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	h := tm.Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }),
	)
	for range 10 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
		if w.Code != http.StatusAccepted {
			t.Fatalf("business request failed: %d", w.Code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = tm.Shutdown(ctx)
	if err != nil && strings.Contains(err.Error(), "private-secret") {
		t.Fatal("shutdown disclosed exporter response")
	}
}

func TestEndpointCredentialsRejectedWithoutDisclosure(t *testing.T) {
	t.Parallel()
	_, err := New(
		context.Background(),
		Options{OTLPEndpoint: "https://user:private-token@example.test/traces?private-user=1"},
	)
	if err == nil {
		t.Fatal("accepted endpoint credentials")
	}
	if strings.Contains(err.Error(), "private-") {
		t.Fatal("endpoint error disclosed credentials")
	}
}
