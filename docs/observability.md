# Platform observability

`internal/observability` owns an explicit runtime. It does not set global OTel
providers or propagators, register default Prometheus collectors, or replace
the process logger. `cmd/zns` creates the runtime explicitly after configuration
validation and supplies it to process services.

```go
telemetry, err := observability.New(ctx, observability.Config{
    Enabled: true, Endpoint: "http://collector:4318", SampleRatio: 0.1,
})
// Handle err, mount telemetry.Handler() at a protected /metrics endpoint,
// and call telemetry.Shutdown(flushContext) during graceful shutdown.
ctx, finish := telemetry.Start(ctx, "model.plan")
// Perform the operation with ctx; call finish(operationErr) exactly once.
```

Disabled tracing uses a no-op provider; metrics and logging still work. Enabled
tracing uses OTLP HTTP protobuf, a bounded batch queue, a three-second export
timeout, and a parent-based ratio sampler. The endpoint cannot contain URL
credentials, query parameters, or fragments. The exporter sends to `/v1/traces`.
Shutdown uses the caller's deadline. Collector failures return static error
messages; collector bodies and transport URLs do not enter SDK error reports.

`HTTPHandler(operation, next)` extracts W3C trace context and preserves optional
HTTP writer interfaces through httpsnoop. It does not swallow panics or change
status codes. `HTTPClient(operation, transport)` clones the request headers,
injects trace context, and strips baggage. Client duration ends when the wrapped
RoundTripper returns response headers; it does not include response-body reads.
Server duration includes handler execution. Neither wrapper records paths,
query strings, headers, bodies, arbitrary methods, or hostnames.

Operations are limited to `telegram.update`, `api`, `server`, `db`, `model.plan`,
`model.skills`, `media.decode`, `sticker.describe`, and `js.run`. Other input maps
to `unknown`. `zns_operations_total` has operation and result labels; results
are `ok`, `error`, `canceled`, and `timeout`. HTTP 5xx counts as error. Latency is
`zns_operation_duration_seconds`, labeled only by operation. These bounds apply
to span names too. Traces contain no SQL, error messages, or user attributes.

Set a pool's connection tracer to `telemetry.PGXTracer()` before constructing
the pool. `RegisterPool(pool)` adds aggregate acquired/idle/constructing gauges
and acquisition-duration counters to that runtime's registry. Register one pool
per runtime; a duplicate registration returns an error. Query instrumentation
ignores SQL, arguments, and connection strings.

## Logging contract

Agent request chains, retries, tool outcomes and sanitized script structure use
the [agent diagnostic log](agent-diagnostics.md). Its bounded operator export
supports model-assisted analysis of repeated failures and unnecessary code.
Memo bodies and raw tool results are excluded. This is separate from aggregate
operation metrics and does not assert a deployed analytics dashboard.

`NewLogger(writer, LogConfig{Level, Secrets})` emits JSON using slog. Levels are
trace (-8), debug, info, warning, and error. `NewHandler` decorates an existing
handler, whose own level filter must also admit the requested levels. Use
contextual logging methods so active span IDs become `trace_id` and `span_id`.
Bound attributes and groups retain their original structure.

Register configured credentials in `Secrets`. Matching values are redacted
from messages, keys, and attributes. Password/token/key/credential fields are
always redacted, including nested groups/maps. Common PII fields are available
only on debug records, not trace records. JWT-like strings expose only header
and payload at debug; their signature is masked. Other levels redact them fully.
URLs are always removed; error values become a static failure message. Unknown
objects are redacted instead of invoking arbitrary string formatting.

Use static log messages. Put all user-controlled text in sensitive structured
fields such as `text`, `email`, `owner`, or `payload`; heuristics cannot identify
every person's name inside arbitrary prose. Debug logs can contain PII and need
restricted access and retention. Redaction bounds text length and recursive
structure; logs are not a replacement for a data-classification policy.

## Alloy operator example

Read-only inspection of the local server-config checkout found Prometheus
scraping/remote-write, cAdvisor and Docker Loki components, but no OTLP receiver.
No configuration or remote service was changed. This secret-free example is for
future operator adaptation, not a deployed configuration:

```alloy
prometheus.scrape "zns" {
  targets = [{ "__address__" = "zns:8080" }]
  metrics_path = "/metrics"
  forward_to = [prometheus.remote_write.metrics.receiver]
}

otelcol.receiver.otlp "zns" {
  http { endpoint = "127.0.0.1:4318" }
  output { traces = [otelcol.processor.batch.zns.input] }
}

otelcol.processor.batch "zns" {
  output { traces = [otelcol.exporter.otlphttp.traces.input] }
}

otelcol.exporter.otlphttp "traces" {
  client { endpoint = "https://trace-backend.example.invalid" }
}
```

Replace the remote-write component reference with the existing local name.
Adjust network binding for the application network and provide backend auth via
the operator's secret mechanism. Keep scrape/receiver endpoints private. Logs
continue through the existing stdout/Docker Loki pipeline. See official Alloy
[scrape](https://grafana.com/docs/alloy/latest/reference/components/prometheus/prometheus.scrape/)
and [OTLP receiver](https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.receiver.otlp/)
documentation. No Grafana or live collector acceptance is claimed.

Package tests exercise a real local OTLP HTTP collector, exported parent-child
spans, contextual log correlation, privacy, bounded scrape labels, disabled
tracing, HTTP interfaces, and pool/query metrics. Dependency evidence is in
[telemetry-dependencies.md](telemetry-dependencies.md).

## Process wiring

Set `log.level` in YAML or `ZNS_LOG__LEVEL` to `trace`, `debug`, `info`, `warning`,
or `error`; the default is `info`. Startup constructs the redacting logger with
database URL, signing key, Telegram token, model key, and worker secrets. A safe
bootstrap logger handles configuration failures without printing raw errors.

All serving commands expose `/metrics`; scrape requests do not count themselves.
The main HTTP handler has a `server` observation. Telegram updates have one
`telegram.update` boundary around `Bot.Handle`, including direct intake and
durable-inbox delivery. Model providers have a `model.plan` boundary, including
scripted and local Codex providers. Core, Telegram, and remote/model HTTP
transports produce `api` child operations while retaining existing timeouts.
Media HTTP calls use `media.decode`. Internal skill selection and sticker
transport require their own narrower integration points.

`store.Open` accepts an optional query tracer, installs it before constructing
the pool, and never echoes a rejected connection string. Runtime registration
adds pool metrics after opening. The PostgreSQL test verifies the tracer at
connection creation and on an actual read-only query; pgx protocol Ping itself
does not emit query-tracer events.

On shutdown, serving commands drain requests and stop/join bot and maintenance
consumers before returning. The command then closes the database pool and
flushes completed telemetry with an uncanceled context bounded by
`shutdown.telemetry_flush`. Existing server-drain tests and focused
configuration, logger, HTTP/model wiring, update-observer, and real PostgreSQL
tracer tests pass. This wiring is built and tested, pending independent QA.
