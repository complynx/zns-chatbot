# Drain-phase diagnostics

The bot emits one JSON record, `application drain finished`, after observing its
public HTTP, private HTTP and worker shutdown callbacks. The existing admission
gate, concurrent shutdown, forced HTTP close, configured drain budget and
five-second watchdog remain unchanged. This adds diagnostics, not a new restart
or delivery policy.

The record has fixed fields:

- `duration_ms`: elapsed monotonic time inside drain, as fractional milliseconds.
- `budget_ms`: the configured drain budget; the default is 1,000 milliseconds.
- `public_http`, `private_http`, `workers`: observed callback classifications.
- `outcome`: the aggregate classification at summary construction.

Statuses are `completed`, `deadline`, `cancelled`, `error` and, for participant
fields only, `unfinished`. A successful callback returned nil. Context deadline
and cancellation errors receive their respective classes; other failures become
`error`. HTTP shutdown errors are retained even when subsequent forced close
succeeds. Expected already-closed errors from that close are ignored.

`unfinished` means that the result was not observed before drain returned. It
does not prove that the goroutine is still running. Already-buffered results are
collected when cancellation wins. The three-slot result channel allows late
callbacks to return without waiting for a receiver.

The aggregate gives precedence to deadline, then cancellation, then error. It
also checks the drain context at summary construction. Thus an already-exhausted
budget remains visible even when callbacks report success. This is a conservative
observation, not proof that a particular callback finished after the deadline.
Successful overall completion logs at INFO; other outcomes log at WARN. No raw
shutdown errors, headers, credentials or payloads are included.

## What this measures

Drain duration excludes the final log write and deferred telemetry/database
cleanup. `completed` means that the three callbacks returned successfully and
the drain context was not exhausted when classified. Worker Stop currently does
not expose every lock-release failure. The summary therefore cannot establish a
clean whole-process exit, exactly-once business completion or a shutdown SLA.

The unchanged watchdog covers cancellation through subsequent deferred cleanup
and exits the process after its five-second timer. A process killed before it can
write the summary may have no final record. Container state and external
monitoring remain necessary. Do not infer a successful shutdown from a missing
summary or replace this final event with a process-local metric that disappears
before a scrape.

With the existing Alloy Docker log pipeline, inspect for example:

```logql
{job="integrations/docker",instance="complynx.net"} | json | msg="application drain finished"
```

Correlate the container and lifecycle timestamps; `duration_ms` itself uses a
monotonic clock. No production log rule, alert route or Grafana configuration is
installed by this change. The existing [Prometheus alert pack](shuttle-alerts.md)
still does not detect hard-kill deadline violations.

Source and owned healthy stop/start acceptance are recorded in the
[QA report](shuttle-shutdown-qa-2026-10-15.md). A separate Linux
[subprocess acceptance](shuttle-watchdog-qa-2026-10-15.md) checks watchdog exit
and disarm during blocked cleanup. Neither establishes worst-case whole-bot
termination latency or external hard-kill detection.

A later isolated actual-bot [blocked-read scenario](shuttle-shutdown-fault-qa-2026-10-15.md)
observed process exit within conservative 233.8304 ms and 229.6554 ms bounds,
with separate successful drain records. The same unfinished task/run/session
survived restart and resumed reading without another run creation. Real IAM was
used with synthetic Loom and a dummy-token Telegram sink; the original stand
remained unchanged. This context-cancellable case does not force the watchdog or
establish a universal worst-case SLA.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
