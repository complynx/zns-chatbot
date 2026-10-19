# Outbox metric plan

Expose deployment-wide counts of unfinished or externally uncertain outbox rows
on the existing private OTel/Prometheus endpoint. Count message chunks, not users,
tasks or logical messages. Do not join task status: a completed task can still
have an unfinished delivery.

Architecture and senior Go discussion selected these fixed state labels:
`pending`, `retryable`, `send_inflight`, `delivery_unknown`, `other`.
Exclude the known terminal outcomes `delivered`, `delivery_blocked` and
`delivery_failed`; permanent failed receipts can remain after task closure.
Fold other persisted states to `other` in SQL before grouping. A zero count does
not establish absence of delivery incidents. Existing attempt telemetry and
operator diagnostics still cover failures.

`delivery_unknown` records an unresolved external outcome. Local task closure
does not rewrite that receipt, so its count can persist afterward. Neither that
bucket nor a pending chunk behind an uncertain earlier chunk is necessarily
automatically actionable. No metric collection may retry a send or change state.

Report oldest age since outbox row creation, using the stored instant and
`statement_timestamp()`. This is not time in the current state, task acceptance
age or runnable latency. Preserve signed future samples through cache storage;
add elapsed cache time before clamping observation to zero. Empty buckets stay zero.

Use a distinct `db.OutboxMetric` and explicit outbox gauges. Match the existing
task snapshot's immutable publication, validation, 30-second cooldown after every
completed attempt, 250 ms acquisition-plus-query context and honest failure
metadata. On failure omit counts/ages, expose failure and retain the last successful
timestamp/freshness. Never substitute an empty healthy backlog or old counts.
No IDs, receipts, payload, error text or arbitrary state labels enter metrics.

Keep the query, cache and callback independent of task metrics so one table lock
does not invalidate both families. The first concrete implementation passed
behavioral checks but lint rejected its duplicated task mechanics. Share a small
private scalar snapshot/gauge mechanism with separate cache instances; retain
typed database adapters, fixed family states and the existing task instruments.
No generic registry or configurable collector framework is required.
The pinned OTel SDK serializes callbacks: when both snapshots need refresh,
collection can incur two 250 ms database budgets plus cancellation/SDK overhead.
Concurrent HTTP scrapes can queue behind collection. This is not a 250 ms endpoint
latency guarantee.

Migration 27 should add only a partial `(state,created_at)` index with exactly the
query's three-state exclusion predicate. Down drops only that index. Ordinary
transactional index creation can block writes during build; it is not an online
migration. The index avoids scanning retained terminal history, but does not bound
the unresolved population. A query exceeding budget fails honestly.

Acceptance requires real PostgreSQL state/folding/chunk/future-age checks;
behavioral scrape cache/failure/recovery/omission tests; actual acquisition and
blocked-query timeout; simultaneous task/outbox collection with independent
health; and index Down/Up preserving rows and deduplication. Review loops, lint,
race gates, documentation and native SQL-only fixture/restart/rollback checks
follow. Fixtures must enqueue no River work or Telegram sends. This plan is now
implemented and accepted within the [documented source/native boundary](shuttle-outbox-metrics.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
