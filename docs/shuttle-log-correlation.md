# Contextual JSON log correlation

The application wraps its existing `slog` JSON handler with
`observability.WithTraceContext`. A contextual log call with a valid current
OpenTelemetry span context gains lowercase hexadecimal `trace_id` (32 characters)
and `span_id` (16 characters). Background/startup logs without a valid span omit
both fields. Existing messages, levels, attributes and output remain unchanged.

Valid unsampled spans also supply IDs. Sampling determines trace export, not
whether IDs exist; a log ID does not prove that a collector received the span.
The wrapper adds no access-log stream, global logger/provider, OTel Logs SDK,
dependency, database write or exporter. Existing structured stdout continues
through the server's Alloy Docker log pipeline.

Only the current span's two IDs are read. Headers, arbitrary context values,
baggage, tracestate, tokens and message/file contents are not copied into logs.
Trace context remains diagnostic and conveys no permissions. This wrapper does
not redact ordinary attributes supplied by a call site; existing safe diagnostic
messages must continue to avoid raw dependency errors and private payloads.

## Handler and call-site contract

The wrapper delegates level filtering and output errors, clones each enriched
record, and keeps derived handlers wrapped. `WithAttrs` and `WithGroup` preserve
normal slog semantics. Correlation attributes follow the active group; they are
top-level in the application's current logger, which uses no groups. Call sites
reserve `trace_id` and `span_id` and should not supply duplicate keys. No custom
deduplication or group rebasing is introduced.

Use `InfoContext`, `WarnContext` or `ErrorContext` with the current work/request
context. The HTTP middleware supplies its server span. A durable task attempt
creates a fresh `task.process` span linked to the persisted acceptance context;
diagnostics after that point use the attempt's IDs. Routing and claiming happen
before the attempt span, and deferred lease-release cleanup captures an earlier
context, so those records cannot promise task-attempt IDs. No lifecycle order or
lease behavior changed to add correlation.

## Inspecting on the server

After authorized deployment, use the existing Docker log source. For example,
replace the diagnostic ID in this LogQL query:

```logql
{job="integrations/docker",instance="complynx.net"} | json | trace_id="<32 hexadecimal characters>"
```

Inspect the matching `span_id` and service/container context. Treat missing IDs as
missing diagnostic context; do not invent a trace or equate that absence with a
business failure. A Grafana derived-field link, collector route and retention
policy need their own operator configuration. No production Loki/Grafana setting
is changed by this implementation.

Both Loom HTTP boundaries and persisted attempt links have separate partial
acceptance. The current Loom async queue starts another trace; JSON log IDs do
not close that [known tracing/recovery boundary](shuttle-tracing-qa-2026-10-15.md).
See the [log acceptance record](shuttle-log-correlation-qa-2026-10-15.md) for the
exact source/native checks and their limits.

Sources: Go's [slog handler contract](https://pkg.go.dev/log/slog#Handler) and
OpenTelemetry's [span context validity](https://pkg.go.dev/go.opentelemetry.io/otel/trace#SpanContext.IsValid).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
