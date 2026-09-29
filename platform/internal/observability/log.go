// Package observability provides local telemetry without global providers.
package observability

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const LevelTrace slog.Level = -8

const redacted = "[redacted]"

type LogConfig struct {
	Level   slog.Level
	Secrets []string
}

type logScope struct {
	group string
	attrs []slog.Attr
}

type safeHandler struct {
	next   slog.Handler
	config LogConfig
	scopes []logScope
	jwt    *regexp.Regexp
	url    *regexp.Regexp
}

// NewLogger filters attributes before writing JSON. Use static message strings;
// arbitrary user text belongs in a sensitive structured field.
func NewLogger(w io.Writer, config LogConfig) *slog.Logger {
	next := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: config.Level, ReplaceAttr: levelName})
	return slog.New(NewHandler(next, config))
}

// NewHandler preserves groups and bound attributes, filtering them at record level.
func NewHandler(next slog.Handler, config LogConfig) slog.Handler {
	config.Secrets = append([]string(nil), config.Secrets...)
	return &safeHandler{next: next, config: config,
		jwt: regexp.MustCompile(`\b[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]{2,}\b`),
		url: regexp.MustCompile(`(?i)\b(?:https?|postgres(?:ql)?|redis)://[^\s<>"']+`)}
}

func levelName(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key != slog.LevelKey {
		return attr
	}
	level, ok := attr.Value.Any().(slog.Level)
	if !ok {
		return attr
	}
	if level == LevelTrace {
		return slog.String(slog.LevelKey, "TRACE")
	}
	if level == slog.LevelWarn {
		return slog.String(slog.LevelKey, "WARNING")
	}
	return attr
}

func (h *safeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.config.Level && h.next.Enabled(ctx, level)
}

func (h *safeHandler) Handle(ctx context.Context, record slog.Record) error {
	next := h.next
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		next = next.WithAttrs(
			[]slog.Attr{
				slog.String("trace_id", span.TraceID().String()),
				slog.String("span_id", span.SpanID().String()),
			},
		)
	}
	private := false
	for _, scope := range h.scopes {
		if scope.group != "" {
			private = private || secretKey(scope.group) || (record.Level != slog.LevelDebug && privateKey(scope.group))
			next = next.WithGroup(h.text(scope.group, record.Level))
		}
		if len(scope.attrs) != 0 {
			next = next.WithAttrs(h.scopedAttrs(scope.attrs, record.Level, private))
		}
	}
	clean := slog.NewRecord(record.Time, record.Level, h.text(record.Message, record.Level), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		clean.AddAttrs(h.scopedAttrs([]slog.Attr{attr}, record.Level, private)...)
		return true
	})
	return next.Handle(ctx, clean)
}

func (h *safeHandler) scopedAttrs(attrs []slog.Attr, level slog.Level, private bool) []slog.Attr {
	if !private {
		return h.attrs(attrs, level, 0)
	}
	result := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		result = append(result, slog.String(h.text(attr.Key, level), redacted))
	}
	return result
}

func (h *safeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.scopes = append(append([]logScope(nil), h.scopes...), logScope{attrs: append([]slog.Attr(nil), attrs...)})
	return &clone
}

func (h *safeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.scopes = append(append([]logScope(nil), h.scopes...), logScope{group: name})
	return &clone
}

func (h *safeHandler) attrs(attrs []slog.Attr, level slog.Level, depth int) []slog.Attr {
	const maxAttrs = 128
	result := make([]slog.Attr, 0, min(len(attrs), maxAttrs))
	for _, attr := range attrs[:min(len(attrs), maxAttrs)] {
		result = append(result, h.attr(attr, level, depth))
	}
	return result
}

func (h *safeHandler) attr(attr slog.Attr, level slog.Level, depth int) slog.Attr {
	const maxDepth = 8
	attr.Key = h.text(attr.Key, level)
	if depth >= maxDepth || secretKey(attr.Key) || (level != slog.LevelDebug && privateKey(attr.Key)) {
		return slog.String(attr.Key, redacted)
	}
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindGroup:
		attr.Value = slog.GroupValue(h.attrs(value.Group(), level, depth+1)...)
	case slog.KindString:
		attr.Value = slog.StringValue(h.text(value.String(), level))
	case slog.KindAny:
		attr.Value = h.anyValue(value.Any(), level, depth+1)
	case slog.KindBool, slog.KindDuration, slog.KindFloat64, slog.KindInt64, slog.KindTime, slog.KindUint64:
		attr.Value = value
	case slog.KindLogValuer:
		attr.Value = slog.StringValue(redacted)
	}
	return attr
}

func (h *safeHandler) anyValue(value any, level slog.Level, depth int) slog.Value {
	switch typed := value.(type) {
	case error:
		return slog.StringValue("operation failed")
	case map[string]any:
		const maxMapAttrs = 128
		attrs := make([]slog.Attr, 0, min(len(typed), maxMapAttrs))
		for key, item := range typed {
			if len(attrs) == maxMapAttrs {
				break
			}
			attrs = append(attrs, slog.Any(key, item))
		}
		return slog.GroupValue(h.attrs(attrs, level, depth)...)
	case string:
		return slog.StringValue(h.text(typed, level))
	case nil:
		return slog.AnyValue(nil)
	default:
		return slog.StringValue(redacted)
	}
}

func (h *safeHandler) text(text string, level slog.Level) string {
	for _, secret := range h.config.Secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, redacted)
		}
	}
	text = h.url.ReplaceAllString(text, "[url redacted]")
	text = h.jwt.ReplaceAllStringFunc(text, func(token string) string {
		// Public dotted operation names are not credentials. Configured secrets
		// were removed above; arbitrary names still follow JWT redaction.
		if safeAgentOperation(token) != diagnosticUnknown {
			return token
		}
		if level != slog.LevelDebug {
			return redacted
		}
		index := strings.LastIndexByte(token, '.')
		return token[:index+1] + redacted
	})
	const maxText = 4096
	if len(text) > maxText {
		text = text[:maxText] + "[truncated]"
	}
	return text
}

func normalizedKey(key string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
}

func secretKey(key string) bool {
	key = normalizedKey(key)
	if key == "xznsderivation" {
		return true
	}
	for _, part := range []string{"password", "secret", "token", "authorization", "cookie", "apikey", "credential", "privatekey", "signature", "dsn"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func privateKey(key string) bool {
	key = normalizedKey(key)
	if key == "id" || key == "ip" {
		return true
	}
	for _, part := range []string{"name", "email", "phone", "address", "userid", "owner", "chatid", "message", "text", "prompt", "content", "body", "payload", "query", "sql", "args", "url", "path", "jwt"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}
