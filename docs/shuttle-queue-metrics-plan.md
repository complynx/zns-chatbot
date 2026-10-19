# River due-job metrics plan

Expose scheduling backlog for the bot's three existing River job kinds, separately
for `available`, `scheduled` and `retryable`. Count only default-queue jobs whose
`scheduled_at` has arrived. Measure oldest lateness from that deadline, never from
job creation, message acceptance or a task's business state.

Pinned River v0.49.0 fetches only due `available` jobs. Due `scheduled` and
`retryable` jobs still need scheduler promotion. The metric therefore describes
due jobs, not guaranteed runnable jobs or worker saturation. Pausing a queue does
not erase backlog. User-waiting tasks still poll through River: their future
snoozes are absent, and an overdue poll reports only its scheduled lateness.

Architecture and Go consultations agreed on a task-owned, read-only SQL adapter
using the configured, quoted schema and actual job `Kind()` methods. It selects
no arguments, metadata or identifiers and joins no business tables. River owns
its migrations and indexes; this slice adds no application migration or fake
SQLC schema. Existing indexes and native plan/timing evidence come first.

Observability receives an aggregate callback. Extend the existing immutable
snapshot mechanics only with an optional fixed kind dimension and count name.
Existing task/outbox names and state-only labels must remain unchanged. The new
family has nine fixed kind/state combinations, zero-filled after successful reads.
Unknown/duplicate combinations invalidate a snapshot. Keep one independent
30-second cache and 250 ms acquisition/query budget, with honest failure/freshness
gauges and no stale counts presented as current after a failed collection.

Acceptance requires preserved existing collector behavior, real River schema and
due/future/state/kind/queue isolation tests, fixed-label exporter checks, timeout
and independent-family checks, full source review/gates, and bounded native queue
behavior on the owned stand. Include an overdue snooze/promotion and backlog drain,
not just inserted-row format checks. No worker semantics, pause controls, scheduler,
new dependency, configuration or speculative index is part of this slice.

This is a plan, not acceptance or production readiness.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
