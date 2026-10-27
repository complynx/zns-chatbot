package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// WithTraceContext adds valid diagnostic IDs to each contextual log record.
// IDs follow the handler's active group; call sites reserve trace_id and span_id.
func WithTraceContext(handler slog.Handler) slog.Handler {
	return traceHandler{handler}
}

type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record = record.Clone()
		record.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return WithTraceContext(h.Handler.WithAttrs(attrs))
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return WithTraceContext(h.Handler.WithGroup(name))
}
