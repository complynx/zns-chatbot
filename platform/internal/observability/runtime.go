package observability

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type Config struct {
	Enabled     bool
	Endpoint    string
	SampleRatio float64
}

const (
	operationFailureCodeLabel            = "code"
	operationFailurePhaseLabel           = "phase"
	operationFailureRetryabilityLabel    = "retryability"
	operationFailureTransportStatusLabel = "transport_status"
	operationResultError                 = "error"
)

type Runtime struct {
	Registry    *prometheus.Registry
	tracer      trace.Tracer
	provider    *sdktrace.TracerProvider
	duration    *prometheus.HistogramVec
	operations  *prometheus.CounterVec
	failures    *prometheus.CounterVec
	propagation propagation.TraceContext
}

func New(ctx context.Context, config Config) (*Runtime, error) {
	r := &Runtime{Registry: prometheus.NewRegistry(), tracer: noop.NewTracerProvider().Tracer("zns")}
	r.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "zns_operation_duration_seconds", Help: "Operation duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{httpFailureOperationLabel})
	r.operations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zns_operations_total", Help: "Completed operations by bounded result.",
	}, []string{httpFailureOperationLabel, "result"})
	r.failures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "zns_operation_failures_total",
		Help: "Operation completion failures by typed provenance; these observations do not authorize replay.",
	}, []string{httpFailureOperationLabel, operationFailurePhaseLabel, operationFailureCodeLabel,
		operationFailureRetryabilityLabel, operationFailureTransportStatusLabel, "provider_code"})
	r.Registry.MustRegister(r.duration, r.operations, r.failures)
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
		var result string
		if kind == trace.SpanKindInternal {
			result = operationErrorClass(err)
		} else {
			result = errorClass(err)
		}
		if err != nil {
			span.SetStatus(codes.Error, result)
			if kind == trace.SpanKindInternal {
				failure := classifyOperationFailure(err)
				r.failures.WithLabelValues(operation, "completion", failure.code, diagnosticUnknown,
					"0", strconv.Itoa(failure.providerCode)).Inc()
				span.SetAttributes(attribute.String("failure.phase", "completion"),
					attribute.String("failure.code", failure.code),
					attribute.String("failure.retryability", diagnosticUnknown),
					attribute.Int("failure.transport_status", 0),
					attribute.Int("failure.provider_code", failure.providerCode))
			}
		}
		r.duration.WithLabelValues(operation).Observe(time.Since(started).Seconds())
		r.operations.WithLabelValues(operation, result).Inc()
	}
}

type operationFailure struct {
	code         string
	providerCode int
}

// Completion observes typed provenance only. A domain status cannot prove SQL,
// a provider code is not an HTTP status, and neither authorizes a replay.
func classifyOperationFailure(err error) operationFailure {
	provider := classifyOperationProviderFailure(err)
	if operationDatabaseFailure(err) {
		// SQL owns the primary class; retain independent bounded wire provenance.
		provider.code = "database_failure"
		if operationErrorIs(err, core.ErrDatabaseSerialization) {
			provider.code = "database_serialization"
		}
		return provider
	}
	// Completion cancellation cannot erase a confirmed provider rejection.
	if provider.providerCode != 0 &&
		(operationErrorIs(err, context.Canceled) || operationErrorIs(err, context.DeadlineExceeded)) {
		return provider
	}
	switch {
	case operationErrorIs(err, context.Canceled):
		return operationFailure{code: httpFailureCanceled}
	case operationErrorIs(err, context.DeadlineExceeded):
		return operationFailure{code: httpFailureTimeout}
	}
	if domain := operationDomainFailure(err); domain != "" {
		provider.code = domain
	}
	return provider
}

// Inspect joined branches before ordinary wrappers so no sibling is skipped.
func operationDatabaseFailure(err error) bool {
	if err == nil || (reflect.ValueOf(err).Kind() == reflect.Pointer && reflect.ValueOf(err).IsNil()) {
		return false
	}
	if operationErrorIs(err, core.ErrDatabase) || operationErrorIs(err, core.ErrDatabaseSerialization) {
		return true
	}
	switch reflect.TypeOf(err) {
	case reflect.TypeFor[*pgconn.PgError]():
		statement, ok := errors.AsType[*pgconn.PgError](err)
		return ok && statement != nil
	case reflect.TypeFor[*pgconn.ConnectError]():
		connection, ok := errors.AsType[*pgconn.ConnectError](err)
		if !ok || connection == nil {
			return false
		}
		if !operationErrorIs(err, context.Canceled) && !operationErrorIs(err, context.DeadlineExceeded) {
			return true
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(joined.Unwrap(), operationDatabaseFailure)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return operationDatabaseFailure(wrapped.Unwrap())
	}
	return false
}

func operationDomainFailure(err error) string {
	if err == nil || (reflect.ValueOf(err).Kind() == reflect.Pointer && reflect.ValueOf(err).IsNil()) {
		return ""
	}
	if reflect.TypeOf(err) == reflect.TypeFor[*core.ProblemError]() {
		problem, ok := errors.AsType[*core.ProblemError](err)
		if !ok || problem == nil {
			return ""
		}
		switch {
		case problem.Status == http.StatusUnauthorized || problem.Status == http.StatusForbidden:
			return "domain_denied"
		case problem.Status >= http.StatusBadRequest && problem.Status < http.StatusInternalServerError:
			return "domain_rejected"
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if code := operationDomainFailure(child); code != "" {
				return code
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return operationDomainFailure(wrapped.Unwrap())
	}
	return ""
}

func operationControlFailure(err error) bool {
	if err == nil || (reflect.ValueOf(err).Kind() == reflect.Pointer && reflect.ValueOf(err).IsNil()) {
		return false
	}
	if reflect.TypeOf(err) == reflect.TypeFor[*telegram.ControlError]() {
		control, ok := errors.AsType[*telegram.ControlError](err)
		return ok && control != nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(joined.Unwrap(), operationControlFailure)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return operationControlFailure(wrapped.Unwrap())
	}
	return false
}

func classifyOperationProviderFailure(err error) operationFailure {
	failure := operationFailure{code: "operation_failed", providerCode: operationProviderCode(err)}
	if failure.providerCode != 0 {
		failure.code = "provider_failure"
		switch failure.providerCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			failure.code = "provider_denied"
		case http.StatusTooManyRequests:
			failure.code = "provider_rate_limited"
		}
	}
	if failure.providerCode == 0 && operationControlFailure(err) {
		failure.code = "provider_control_blocked"
	}
	return failure
}

// Match concrete nodes before AsType so custom As cannot fabricate wire provenance.
// Skip invalid nodes so valid wire provenance in a sibling survives.
func operationProviderCode(err error) int {
	if err == nil || (reflect.ValueOf(err).Kind() == reflect.Pointer && reflect.ValueOf(err).IsNil()) {
		return 0
	}
	if reflect.TypeOf(err) == reflect.TypeFor[*telegram.APIError]() {
		if api, ok := errors.AsType[*telegram.APIError](err); ok && api != nil &&
			api.Code >= http.StatusContinue && api.Code <= maxHTTPTransportStatus {
			return api.Code
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if code := operationProviderCode(child); code != 0 {
				return code
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return operationProviderCode(wrapped.Unwrap())
	}
	return 0
}

// Targets are private canonical comparable database and context sentinels only.
// Preserve their Is contract while skipping nil nodes before actual unwrapping.
func operationErrorIs(err, target error) bool {
	if err == nil || (reflect.ValueOf(err).Kind() == reflect.Pointer && reflect.ValueOf(err).IsNil()) {
		return false
	}
	if reflect.TypeOf(err) == reflect.TypeOf(target) && reflect.ValueOf(err).Equal(reflect.ValueOf(target)) {
		return true
	}
	if match, ok := err.(interface{ Is(error) bool }); ok && match.Is(target) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return slices.ContainsFunc(joined.Unwrap(), func(child error) bool { return operationErrorIs(child, target) })
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return operationErrorIs(wrapped.Unwrap(), target)
	}
	return false
}

func operationErrorClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case operationErrorIs(err, context.Canceled):
		return httpFailureCanceled
	case operationErrorIs(err, context.DeadlineExceeded):
		return httpFailureTimeout
	default:
		return operationResultError
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
