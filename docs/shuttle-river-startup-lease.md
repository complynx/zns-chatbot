# River startup/stop lease observation

On 2026-10-15 a new queue-metric integration fixture exposed an intermittent
leadership handover delay in the pinned River v0.49.0 path. This is separate from
the metrics feature. It has not been fixed or reported as resolved upstream.

The fixture migrated an isolated schema, started the ordinary bot service, stopped
it immediately, then started a second River client with one controlled worker.
The worker ran an available job, but the second client sometimes lost every
leadership bid during the ten-second assertion window. Its overdue scheduled job
was not promoted. Disabling production startup jitter with River's `TestOnly`
setting did not fix this: repeated checks still failed.

SQL readback after the first service stopped confirmed its remaining lease. In
one failed run the old leader ID ended in `07_29_33_902149`, while the new client
ended in `07_29_33_977997`. The old lease was elected at 07:29:33.964171 UTC and
expired at 07:29:48.964171 UTC. It remained unchanged at failure. A second failed
run showed the same pattern. Passing runs had no lease after the initial stop,
and the new client won leadership and started its scheduler.

This proves the observed lease delay. A cancellation/ambiguous-election-commit
path is a hypothesis, not a proven internal cause: the pinned elector installs
resignation cleanup after a successful leadership term, while election commit
may return a cancellation error. The cached module and archived source checksums
matched for the relevant maintainer/scheduler files. More focused upstream
investigation is required before attributing the exact commit race.

No product scheduler, election, timeout or lease code changed. No lease was deleted
or shortened. The metrics fixture now prepares its store without first starting
another River client. It starts exactly one native client, so it tests queue
promotion/backlog/drain/snooze independently of this unrelated handover.
That fixture change does not establish a fast production handover. Startup
cancellation remains unresolved. The separate expiry-recovery acceptance below
covers a synthetic abandoned lease, not the original cancellation race.

Retained ignored logs are under `.cache/shuttle-work/container-results/`:
`zinusya-queue-worker-jitter-qa-20261015.log`, both worker diagnosis logs, and
`zinusya-queue-worker-lease-proof-qa-20261015.log`. The latter contains the lease
readbacks and both failed new-client elections. Earlier failures remain intact.

## Owned native expiry-recovery acceptance

The actual bot image
`sha256:d2a10ad3189dd66ecb1c6a09ec373b77020c98f75a4eaf3e50dd54360e0c2ca8`
passed a separate bounded probe on `shuttle-live-test`, using default production
River timings. No Go source, election configuration or scheduler code changed.
After stopping the bot and verifying the leader table was empty, the fixture
inserted one synthetic abandoned lease with the native 15-second lifetime and
two overdue closed-task no-op jobs: one available and one scheduled. No existing
lease was overwritten. The probe never deleted, renewed or shortened a lease.

The seed lease ran from 08:02:50.162682 to 08:03:05.162682 UTC on 2026-10-15.
At 08:02:52.769627 the same leader ID and exact lease timestamps remained active:
available job 89 had completed at attempt 1, while scheduled job 90 remained
scheduled at attempt 0. Thus a live producer could execute available work before
the new process held maintainer leadership.

River naturally acquired successor leadership at 08:03:07.768413, after seed
expiry. The same scheduled job 90 was attempted at 08:03:10.544311 and observed
completed at attempt 1 by 08:03:10.880340: 5.718 seconds after expiry. Both jobs
had no error history. This is one bounded observed recovery, not a maximum
production recovery time. It does not reproduce or fix the startup/stop race.

Cleanup stopped the bot, removed exactly the two stamped jobs and their separate
task/conversation, then restarted the same image. Hashes of 22 selected tables,
Mini App origin and empty queue metrics matched the accepted baseline. The
fixture used no Telegram sends or IAM/Loom inference. Checkpoint:
`.cache/shuttle-live-test/native-river-lease-recovery-checkpoint.json`.
The helper was independently reviewed; review fixes ensure restart is attempted
even when initial stopping, deletion or checkpoint writing fails. Its final
fresh review found no actionable findings.

The operational distinction is important: application readiness and available-job
execution do not prove that scheduled/retryable promotion has resumed. Use the
separate queue states and collection health/freshness when investigating delayed
work. Preserve leases and the original evidence while diagnosing a handover.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
