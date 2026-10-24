# Confirmed final-response latency

The private Prometheus histogram
`zinusya_response_confirmed_duration_seconds` measures root task row creation to
the persisted confirmation of every chunk of its final response. It includes
queueing, retries, service outages, bot restarts and intentional user/approval
waits. It is neither active processing time nor a per-incoming-message or
business-completion SLI. A Telegram receipt establishes API acceptance, not that
a person read the response.

Only newly instrumented `new_run` tasks with a nonempty Loom run ID, completed
state and a nonempty final response qualify. Silent control tasks, local replies,
intermediate questions/approvals, notifications, unrelated avatar delivery,
failed/cancelled tasks and empty results do not produce successful samples.
Several accepted control messages can contribute to one root run; their eventual
answer belongs to that original task, rather than a separate response per input.

## Durable boundaries

Migration 32 adds four nullable task fields. New intake records
`response_started_at` with PostgreSQL `clock_timestamp()` while creating the
task row. This is before the intake transaction commits; it is not an exact
commit timestamp or Telegram's original message time. Old rows and old writers
leave the field NULL. There is no historical backfill.

The explicit `response_first_chunk_seq` / `response_last_chunk_seq` range is
recorded atomically with root completion and final outbox insertion. Existing
rows are located by their exact outgoing deduplication keys; key syntax is not
used to infer message purpose. The persisted range must be nonempty and
contiguous. It excludes already-created intermediate prompts.

The successful delivery-receipt transaction locks the task before the outbox row,
applies the existing attempt CAS, and sets `response_delivered_at` only once when
every declared final chunk exists and is confirmed delivered. The winning receipt
must itself belong to that range. Receipt and timing commit together. Pending,
retryable, inflight, unknown, blocked or failed chunks never receive success
credit. Missing chunks also prevent completion. No index, historical scan,
sampler or persistent metric accumulator is added.

Negative or nonfinite clock intervals are omitted from the histogram instead of
being clamped to zero. Timestamps remain evidence in PostgreSQL; they do not
replace monotonic measurements of an individual worker callback.

## Observation and rollback limits

The histogram is process-local and best effort. Only the transaction that first
commits complete final delivery returns a sample to the observer. Repeated or
stale receipts cannot produce another sample. A restart resets exported counters
and does not replay historical observations. A crash between DB commit and OTel
recording can lose that observation while preserving its timestamps. Scrape/export
loss is also possible. There is no exactly-once telemetry export promise.

Buckets use seconds: 1, 5, 15, 30, 60, 300, 900, 3600, 21600, 86400, 604800 and
the implicit infinity bucket. No user, task, run, event or receipt identifier is
a metric label. Long user waits can legitimately dominate quantiles; do not use
this histogram as a processing-only latency alert or an availability denominator.

Apply migration 32 before the executable. Binary rollback keeps these additive
columns; an old executable does not record the new boundaries. Do not reconstruct
delivery times from already-delivered rows after re-upgrading. Schema rollback
refuses to erase any timing facts, including accepted starts, until they have
been explicitly exported and handled. Empty schema Down/Up and guard behavior
are checked separately. The rollback takes an exclusive task-table lock before
checking for facts, so concurrent intake cannot commit timing data between the
check and column removal. A real PostgreSQL concurrency test verifies this.
The agent manifest remains `mvp-31`.

Service tests cover OTel after confirmed delivery, restart without
metric replay, and Telegram acceptance followed by receipt-persistence failure.
This document does
not claim production readiness or automatic recovery from that provider pause.
