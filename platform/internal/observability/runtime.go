package observability

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type Config struct {
	Enabled     bool
	Endpoint    string
	SampleRatio float64
}

type Runtime struct {
	Registry    *prometheus.Registry
	tracer      trace.Tracer
	provider    *sdktrace.TracerProvider
	duration    *prometheus.HistogramVec
	operations  *prometheus.CounterVec
	propagation propagation.TraceContext
}

func New(ctx context.Context, config Config) (*Runtime, error) {
	r := &Runtime{Registry: prometheus.NewRegistry(), tracer: noop.NewTracerProvider().Tracer("zns")}
	r.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "zns_operation_duration_seconds", Help: "Operation duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})
	r.operations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zns_operations_total", Help: "Completed operations by bounded result.",
	}, []string{"operation", "result"})
	r.Registry.MustRegister(r.duration, r.operations)
	if !config.Enabled {
		return r, nil
	}
	if err := r.enable(ctx, config); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Runtime) enable(ctx context.Context, config Config) error {
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" {
		return errors.New("invalid telemetry endpoint")
	}
	if math.IsNaN(config.SampleRatio) || config.SampleRatio < 0 || config.SampleRatio > 1 {
		return errors.New("invalid telemetry sample ratio")
	}
	const exportTimeout = 3 * time.Second
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(config.Endpoint),
		otlptracehttp.WithURLPath("/v1/traces"), otlptracehttp.WithHeaders(map[string]string{}),
		otlptracehttp.WithTimeout(exportTimeout), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	if err != nil {
		return errors.New("telemetry exporter initialization failed")
	}
	r.provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(safeExporter{next: exporter}),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.SampleRatio))),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "zns-chatbot"))))
	r.tracer = r.provider.Tracer("zns-chatbot")
	return nil
}

// Handler exposes only this runtime's registry. Restrict access at deployment.
func (r *Runtime) Handler() http.Handler {
	return promhttp.HandlerFor(r.Registry, promhttp.HandlerOpts{})
}

// Start creates a bounded operation span and records metrics even with tracing disabled.
// The finish function must be called exactly once; errors are classified, never serialized.
func (r *Runtime) Start(ctx context.Context, operation string) (context.Context, func(error)) {
	return r.start(ctx, operation, trace.SpanKindInternal)
}

func (r *Runtime) start(ctx context.Context, operation string, kind trace.SpanKind) (context.Context, func(error)) {
	operation = operationName(operation)
	ctx, span := r.tracer.Start(ctx, operation, trace.WithSpanKind(kind))
	started := time.Now()
	return ctx, func(err error) {
		defer span.End()
		result := errorClass(err)
		if err != nil {
			span.SetStatus(codes.Error, result)
		}
		r.duration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		r.operations.WithLabelValues(operation, result).Inc()
	}
}

// safeExporter keeps collector response bodies and transport URLs out of the
// SDK's asynchronous error reporting without changing process-global handlers.
type safeExporter struct{ next sdktrace.SpanExporter }

func (e safeExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.next.ExportSpans(ctx, spans); err != nil {
		return errors.New("telemetry export failed")
	}
	return nil
}

func (e safeExporter) Shutdown(ctx context.Context) error {
	if err := e.next.Shutdown(ctx); err != nil {
		return errors.New("telemetry exporter shutdown failed")
	}
	return nil
}

// Shutdown flushes completed spans within the caller's deadline.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r.provider == nil {
		return nil
	}
	if err := r.provider.Shutdown(ctx); err != nil {
		return errors.New("telemetry shutdown failed")
	}
	return nil
}

func operationName(operation string) string {
	switch operation {
	case "telegram.update",
		"api",
		"server",
		"db",
		"model.plan",
		"model.knowledge_assessment",
		"model.history_summary",
		"model.broadcast_name",
		"model.skills",
		"media.decode",
		"sticker.describe",
		"js.run":
		return operation
	default:
		return "unknown"
	}
}

func errorClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "error"
	}
}
