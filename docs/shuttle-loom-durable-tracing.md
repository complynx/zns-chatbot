# Durable trace context across Loom queue delivery and recovery

Status, 2026-10-22: implementation merged and published in Loom main
`b2b8d429156adcb4d667cb950a6c0a8d97f09cce`. Merged-main CI and an independent
local rerun of nine focused Linux/race/PostgreSQL/OTLP tests passed.
[Evidence and remaining consumer gate](shuttle-loom-durable-tracing-qa-2026-10-22.md).
The published image is installed on the owned primary stand through the
separately accepted [primary upgrade](shuttle-profile-name-sync-qa-2026-10-22.md#owned-primary-upgrade-with-ingress-fenced).
That installation is distinct from the isolated consumer fixtures. Separate
consumer gates passed [steering and bot restart](shuttle-consumer-tracing-qa-2026-10-22.md)
and [Loom restart](shuttle-consumer-loom-restart-tracing-qa-2026-10-22.md).
The [approval trace gate](shuttle-consumer-approval-tracing-qa-2026-10-22.md)
also passes original-operation consent after both process restarts, including
the exact backend commit HTTP span in the consumed-consent trace.
Primary-backend visibility remains open.
The original request below describes the earlier `34f46468` source boundary;
it is retained as history, not a description of the new implementation.

Upstream request: [Loom #27](https://github.com/complynx/loom/issues/27).

## Original source boundary (34f46468)

[Loom HTTP telemetry](https://github.com/complynx/loom/blob/34f4646832748efd455828f6804c6274027db659/internal/telemetry/otel.go#L190) extracts W3C context on HTTP and injects it into
outgoing HTTP; it explicitly does not forward baggage. [runWorker.Work](https://github.com/complynx/loom/blob/34f4646832748efd455828f6804c6274027db659/internal/app/runtime.go#L41) starts `run_execution` from the River worker context.
`executeRun` contains `run_id` and `delivery_fence`. The inspected
[queue insertion](https://github.com/complynx/loom/blob/34f4646832748efd455828f6804c6274027db659/internal/app/queue_recovery.go#L16) and River configuration show no trace middleware. The inspected run/queue/store
paths do not show a persisted origin trace carrier or restored-attempt links.
Existing Zinusya QA proves HTTP propagation separately; it does not prove the
causal link through durable River delivery. This is a feature/contract proposal,
not a new runtime reproduction or a claim that no worker spans exist.

## Required behavior, wire/storage format open to maintainer

1. Preserve a bounded validated W3C origin span context when accepting durable
   work, atomically with run/delivery state where appropriate. Persist values,
   not SDK span objects; do not persist baggage, arbitrary headers, credentials,
   prompts, documents or user text. Legacy/missing/invalid trace data must not
   prevent authorized business execution or create new operations.
2. Restore that causal context for each execution attempt after ordinary queue
   delivery and process restart. Each attempt needs a distinct span ID. Use the
   original trace or a documented new-trace-with-link policy that allows an
   operator to follow ingress -> queue -> execution -> module/provider HTTP.
   Specify treatment of later steering/input traces; they must not silently
   erase the original causal identity.
3. Recovery after SIGKILL must retain original run/business IDs and link the
   recovered attempt to the origin and prior attempt where available. Trace
   state is diagnostic data: it cannot authorize Resume, settle unknown work,
   bypass consent, or alter delivery/lease fencing. Avoid a long open span for
   the entire conversation; bound persisted metadata per run/attempt.
4. A documented span-link policy is acceptable. OpenTelemetry messaging
   conventions recommend links to message-creation context and allow a
   documented parent choice for a single message. This request does not mandate
   one long trace or a particular database schema.
5. Keep existing sampling configuration. The Zinusya stand uses full sampling;
   this request does not authorize lowering it. Preserve the existing no-baggage
   behavior and safe correlation in logs.

## Acceptance requested

Use an OTLP receiver plus real PostgreSQL/River and known synthetic incoming
trace context. Verify exported relationships across an actual queue boundary
while the ingress request has already completed, including module/provider
outgoing context. Then cover graceful restart, an expired-executor SIGKILL
recovery and a later steering input with a different trace. Assert distinct
attempt spans, original run/task identity, correct links and unchanged dedup/
unknown exposure. Check missing/invalid/legacy carrier fallback. Retain original
failed observations; source search alone cannot establish a runtime failure.

Zinusya can add consumer acceptance on its own stand after the contract/image
is available; no changes to the Loom developer checkout or stand are requested.

## References and integration ownership

- [OpenTelemetry context propagation](https://opentelemetry.io/docs/concepts/context-propagation/).
- [Messaging span relationships](https://opentelemetry.io/docs/specs/semconv/messaging/messaging-spans/#messaging-spans).
  The inspected messaging conventions are marked Development; they are guidance,
  not a claim that an unstable wire/storage schema is mandatory.
- Zinusya's [existing trace evidence and open boundary](shuttle-tracing-qa-2026-10-15.md#open-trace-boundary).

The bot already persists its accepted `traceparent` / `tracestate` and links task
attempts to acceptance. The remaining Loom queue/attempt policy belongs to Loom;
Zinusya will follow the documented contract when testing the published image.
