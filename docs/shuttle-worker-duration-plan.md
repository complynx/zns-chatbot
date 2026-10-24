# Worker callback duration plan

Next bounded observability slice: one process-local OTel histogram,
`zinusya.worker.attempt.duration`, unit seconds, exported as
`zinusya_worker_attempt_duration_seconds`.
Architecture and Go consultations
agreed on the return-only contract below.

Instrument the three outer River `Work` methods for `shuttle_task`,
`zinusya_payment_notification` and `zinusya_pair_notification`. Start immediately
before the existing service call, record once after it returns, then return the
identical error. Ordinary post-call recording avoids reporting a panic as a nil
return. Do not add recovery behavior or alter existing counters.

The measurement includes callback-owned PostgreSQL/IAM/Loom/Telegram waits,
receipt persistence and detached lease/finalization cleanup. It excludes queue
wait, scheduled snooze time, River persistence after the callback, still-running
callbacks, panics and process death. It does not measure CPU utilization or
end-to-end business completion.

Use fixed `kind` and `outcome` labels only. Fold unknown values into `other`.
Return classification stays in the task package and follows River's precedence:

- `returned`: nil callback return, including no-op or persisted uncertainty.
- `snoozed`: returned error contains `*river.JobSnoozeError`.
- `cancelled`: returned error contains `*river.JobCancelError`.
- `error`: every other non-nil return, including raw context errors.

Check snooze before explicit cancellation, matching River v0.49. A snooze does
not consume River's attempt allowance; “attempt” here means callback invocation.
Do not override the classification from parent-context state or infer an internal
timeout from elapsed time. Task logic can translate child-context timeouts into
snoozes; River can also change final job disposition after the callback returns.
The histogram describes the callback return, not that later disposition.

The observability package owns the histogram, bounded labels and explicit latency
buckets in seconds. No identifiers, error messages, payloads, queue strings or
attempt numbers become labels. No new dependency, migration, counter, in-flight
gauge, scrape-time query or generic worker decorator is needed.

Behavioral proof should cover a completed task, scheduled snooze, returned error,
and actual payment/pair callback paths with their persisted outcomes. Scrape the
private metrics handler while an owned peer is blocked: an unfinished invocation
must contribute no observation. Release it, then verify a single observation and
duration covering the gated interval without tight timing assertions. Verify
unknown-label folding, secret canaries, existing counters, and explicit/wrapped
River error classification. Do not require production callbacks to invent an
explicit cancellation result solely to exercise that label.

Native fixtures must have reviewed safe
effects and exact cleanup/preservation accounting; never execute arbitrary jobs
against business workers just to obtain a metric.
Document any native proof
limits.
Queue wait, utilization, dashboards, SLOs and tracing redesign remain
outside this slice.
