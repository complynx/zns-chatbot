// Package observability owns the application's scoped telemetry providers.
package observability

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Options struct {
	OTLPEndpoint string
	ServiceName  string
	SampleRatio  float64
	MaxQueueSize int
	Timeout      time.Duration
}

const otherLabel = "other"
const errorLabel = "error"

const maxExportBatchSize = 128

type Telemetry struct {
	traces          *sdktrace.TracerProvider
	meters          *sdkmetric.MeterProvider
	tracer          trace.Tracer
	propagator      propagation.TextMapPropagator
	handler         http.Handler
	operations      metric.Int64Counter
	httpRequests    metric.Int64Counter
	httpDuration    metric.Float64Histogram
	workerDuration  metric.Float64Histogram
	responseLatency metric.Float64Histogram
	timeout         time.Duration
}

func New(ctx context.Context, opts Options) (*Telemetry, error) {
	if math.IsNaN(opts.SampleRatio) || opts.SampleRatio < 0 || opts.SampleRatio > 1 || opts.MaxQueueSize < 0 ||
		opts.Timeout < 0 {
		return nil, errors.New("invalid telemetry limits")
	}
	if opts.Timeout == 0 {
		opts.Timeout = time.Second
	}
	if opts.MaxQueueSize == 0 {
		opts.MaxQueueSize = 256
	}
	if opts.ServiceName == "" {
		opts.ServiceName = "zinusya"
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithoutTargetInfo(),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, errors.New("initialize metrics exporter")
	}
	res := resource.NewSchemaless(attribute.String("service.name", opts.ServiceName))
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithResource(res))
	traceOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.SampleRatio))),
	}
	if opts.OTLPEndpoint != "" {
		u, parseErr := url.Parse(opts.OTLPEndpoint)
		if parseErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
			u.RawQuery != "" ||
			u.Fragment != "" {
			_ = meters.Shutdown(ctx)
			return nil, errors.New("invalid telemetry endpoint")
		}
		if u.Path == "" || u.Path == "/" {
			u.Path = "/v1/traces"
		}
		exp, exportErr := otlptracehttp.New(
			ctx,
			otlptracehttp.WithEndpointURL(u.String()),
			otlptracehttp.WithTimeout(opts.Timeout),
		)
		if exportErr != nil {
			_ = meters.Shutdown(ctx)
			return nil, errors.New("initialize trace exporter")
		}
		traceOpts = append(
			traceOpts,
			sdktrace.WithBatcher(
				redactedExporter{exp},
				sdktrace.WithMaxQueueSize(opts.MaxQueueSize),
				sdktrace.WithMaxExportBatchSize(min(maxExportBatchSize, opts.MaxQueueSize)),
				sdktrace.WithExportTimeout(opts.Timeout),
			),
		)
	}
	traces := sdktrace.NewTracerProvider(traceOpts...)
	t := &Telemetry{
		traces:     traces,
		meters:     meters,
		tracer:     traces.Tracer("zinusya"),
		propagator: propagation.TraceContext{},
		handler:    promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		timeout:    opts.Timeout,
	}
	if err = t.initInstruments(meters.Meter("zinusya")); err != nil {
		_ = t.Shutdown(ctx)
		return nil, errors.New("initialize telemetry instruments")
	}
	return t, nil
}

// MetricsHandler belongs on the private listener, outside business middleware.
func (t *Telemetry) MetricsHandler() http.Handler              { return t.handler }
func (t *Telemetry) Tracer() trace.Tracer                      { return t.tracer }
func (t *Telemetry) Propagator() propagation.TextMapPropagator { return t.propagator }

// Inc deliberately folds all unrecognised label values into one series.
func (t *Telemetry) Inc(component, outcome string) {
	t.IncContext(context.Background(), component, outcome)
}

// IncContext records an outcome with the operation's diagnostic context.
func (t *Telemetry) IncContext(ctx context.Context, component, outcome string) {
	t.operations.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("component", boundedComponent(component)),
			attribute.String("outcome", boundedOutcome(outcome)),
		),
	)
}

// Start never records error text, which may contain credentials or user content.
//
//nolint:spancheck // The returned completion function owns and ends this span.
func (t *Telemetry) Start(ctx context.Context, operation string) (context.Context, func(error)) {
	ctx, span := t.tracer.Start(ctx, boundedComponent(operation))
	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			if err != nil {
				span.SetStatus(codes.Error, "operation failed")
			}
			span.End()
		})
	}
}

// Middleware extracts W3C context without recording headers, URLs or user IDs.
func (t *Telemetry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ctx := t.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		method := boundedMethod(r.Method)
		ctx, span := t.tracer.Start(
			ctx,
			"http.server",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("http.request.method", method)),
		)
		defer span.End()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))
		outcome := "success"
		if rw.status >= http.StatusInternalServerError {
			outcome = errorLabel
			span.SetStatus(codes.Error, "request failed")
		} else if rw.status >= http.StatusBadRequest {
			outcome = "rejected"
		}
		span.SetAttributes(attribute.Int("http.response.status_code", rw.status))
		attrs := metric.WithAttributes(attribute.String("method", method), attribute.String("outcome", outcome))
		t.httpRequests.Add(ctx, 1, attrs)
		t.httpDuration.Record(ctx, time.Since(started).Seconds(), attrs)
	})
}

// Shutdown shares the caller's deadline across providers and caps export time.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	traceErr := t.traces.Shutdown(ctx)
	metricErr := t.meters.Shutdown(ctx)
	if traceErr != nil || metricErr != nil {
		return errors.New("telemetry shutdown failed")
	}
	return nil
}

type redactedExporter struct{ sdktrace.SpanExporter }

func (e redactedExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.SpanExporter.ExportSpans(ctx, spans) != nil {
		return errors.New("trace export failed")
	}
	return nil
}
func (e redactedExporter) Shutdown(ctx context.Context) error {
	if e.SpanExporter.Shutdown(ctx) != nil {
		return errors.New("trace exporter shutdown failed")
	}
	return nil
}

type responseWriter struct {
	http.ResponseWriter

	status int
	wrote  bool
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func boundedComponent(s string) string {
	switch s {
	case "telegram",
		"queue",
		"worker",
		"loom",
		"iam",
		"token",
		"db",
		"http",
		"shutdown",
		"poll",
		"webhook",
		"deliver",
		"notifications",
		"pair_notifications",
		"order_notifications", "knowledge_notifications", "avatar_deliveries",
		"massage_reminders",
		"massage_specialist_notices",
		"execute",
		"renew",
		"identity",
		"api":
		return s
	default:
		return otherLabel
	}
}
func boundedOutcome(s string) string {
	switch s {
	case "success",
		errorLabel,
		"rejected",
		"retry",
		"timeout",
		"accepted",
		"duplicate",
		"hit",
		"miss",
		"expired",
		"cancelled",
		"queued",
		"completed",
		"failed":
		return s
	default:
		return otherLabel
	}
}
func boundedMethod(s string) string {
	switch s {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return s
	default:
		return otherLabel
	}
}

func (t *Telemetry) initInstruments(meter metric.Meter) error {
	var err error
	t.operations, err = meter.Int64Counter("zinusya.operations", metric.WithDescription("Operation outcomes"))
	if err == nil {
		t.httpRequests, err = meter.Int64Counter(
			"zinusya.http.requests",
			metric.WithDescription("Business HTTP requests"),
		)
	}
	if err == nil {
		t.httpDuration, err = meter.Float64Histogram("zinusya.http.duration", metric.WithUnit("s"))
	}
	if err == nil {
		t.workerDuration, err = newWorkerDuration(meter)
	}
	if err == nil {
		t.responseLatency, err = newResponseLatency(meter)
	}

	return err
}
