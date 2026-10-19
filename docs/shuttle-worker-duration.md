# Worker callback duration

The OTel histogram `zinusya.worker.attempt.duration` uses seconds. The private
Prometheus endpoint exports `zinusya_worker_attempt_duration_seconds_bucket`,
`_sum` and `_count`. It measures one normally returned River `Work` callback.
It does not measure task completion, CPU use or end-to-end user latency.

The three instrumented callback kinds are `shuttle_task`,
`zinusya_payment_notification` and `zinusya_pair_notification`. The internal
`shuttle_task` queue kind is retained for durable compatibility. Metric names
use the bot identity, Zinusya.

Timing starts immediately before the service call and is recorded immediately
after it returns, including callback-owned database, IAM, Loom and Telegram
waits, receipt persistence and detached lease cleanup. Queue wait, future
snooze delay and River's post-callback persistence are excluded. A still-running
callback, panic or process death contributes no observation. Instrumentation
does not recover panics or change the returned error.

Only fixed `kind` and `outcome` labels are allowed. Unknown input is folded into
`other`; user IDs, run IDs, task IDs, errors, payloads and attempt numbers never
become labels. Outcomes describe the callback return:

- `returned`: nil, including an already-finished task, missing notification or
  notification whose delivery uncertainty was durably recorded.
- `snoozed`: an error containing River's `JobSnoozeError`.
- `cancelled`: an error containing River's `JobCancelError`.
- `error`: any other non-nil error, including raw context cancellation or deadline.

Snooze takes precedence over explicit cancellation, matching River v0.49.
Parent-context state does not override the returned error. Task logic can
translate an internal timeout to snooze, and River can subsequently choose a
different job disposition. A snooze does not consume River's attempt allowance;
“attempt” in this metric means callback invocation.

Explicit bucket boundaries in seconds are 0.005, 0.01, 0.025, 0.05, 0.1, 0.25,
0.5, 1, 2, 5, 10, 15, 20, 22, 30 and 60, plus Prometheus's infinity bucket.
The 20/22-second boundaries cover the default business/River timeout settings;
callback-owned detached cleanup can extend the measured time. These boundaries
are observation buckets, not timeout guarantees.

The histogram is process-local and resets on bot restart. It adds no database
query during scraping, migration or dependency. Existing operation and
notification counters retain their semantics. Use unresolved-task, outbox and
due-queue snapshots for durable work status; a duration count cannot establish
that work was delivered or money confirmed. Compare `_sum` and `_count` only
over matching labels and time windows. Concurrent callbacks can overlap.

The implementation and verification sequence is tracked in the
[bounded plan](shuttle-worker-duration-plan.md). Source checks cover all three
actual callbacks with PostgreSQL/River and HTTP peers, blocked execution before
return, IAM snooze, corrupt-checkpoint errors and unknown delivery. Native
acceptance and all 225 integration race scenarios are recorded in the
[acceptance record](shuttle-worker-duration-qa-2026-10-15.md). Native proof uses
safe no-op callback paths; it does not repeat source delivery/failure tests.
Dashboards, SLOs, utilization and tracing redesign remain outside this slice.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
