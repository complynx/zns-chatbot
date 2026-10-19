# Blocked-read shutdown and task recovery QA — 2026-10-15

This scenario checks the actual bot process with an unfinished durable task and
a context-aware blocked Loom read. No production code, schema or dependency
changed. Architecture and senior Go consultation selected this acceptance gap
before the harness was written.

The owned accepted schema-32 / `mvp-32` image was reused:
`sha256:afb2450bf30ca7baf98c13b785f835b3ba55b9df64a8c4532c3dead7e79f0b3b`.
Its 213 production-source hashes and the reviewed harness hashes were unchanged
through execution. The preceding verified 259-scenario race inventory therefore
remains source-matched; this slice adds native evidence rather than claiming a
new full test run.

## Isolation and review

A new 256 MiB PostgreSQL container received the normal migrations and a copy of
only the owned stand's users table, so the existing owner could use real Zitadel
without creating another identity. No tasks, conversations, outbox, payment
notifications or historical archives were copied. A separate application
container used the accepted executable and startup identity settings.

Loom was a narrow local protocol fixture, not the real Loom service. It returned
one session and one running run, then held only reads of that run until explicit
release. The bot and fixture shared a network namespace with distinct listening
ports. Telegram used a loopback counting/rejecting sink and an explicitly dummy
token. The actual test bot token was overridden before startup.

Independent harness reviews found a non-loopback HTTP Telegram URL that would
fail configuration validation, possible reuse of an earlier drain summary, and
insufficient checks for failed drain components. These were fixed before the
first execution. A fresh independent review then found no actionable findings.
No application validation rule was weakened.

## Observation

One synthetic ordinary-owner update entered through local webhook HTTP and the
normal durable intake path. There was no Telegram Web send. The fixture confirmed
an outstanding held `GET` only after the run/session IDs had been saved in the
task. The harness recorded task, checkpoint, lease/fence and River job state.

It started an independent `docker wait`, dispatched actual SIGTERM and observed
process termination. It did not use `docker stop --time 5` or its fallback kill.
An independent twelve-second rescue limit existed but was not used.

The first stop had a conservative signal-to-observed-exit upper bound of
**233.8304 ms**, including Docker command dispatch and wait-observation overhead.
Signal dispatch itself took 198.5006 ms; observed drain took **5.260281 ms**.
The bot exited 0 without OOM. Its new drain summary classified public HTTP,
private HTTP, workers and aggregate outcome as completed under the 1,000 ms
budget. The fixture observed cancellation of the held connection.

The stopped database retained the original task, run, auth session and snapshot;
the task lease was released. Restarting the same container/image while the
fixture was still blocked produced a fresh read of the same run. The task fence
advanced from 2 to 3 without another session bootstrap or run creation.

After explicit release, the fixture returned running version 2. The same task
persisted that fresh observation and a higher task version. There was exactly
one task and inbox record, zero outbox records, one session bootstrap, one run
create, two reads, one cancelled read, and one successful read response at that
checkpoint. Telegram and unexpected-route counts were both zero.

A second actual SIGTERM after recovery had a conservative observed process-exit
upper bound of **229.6554 ms** and drain **7.202632 ms**. It also exited 0 without
OOM and emitted its own additional all-completed summary. The harness required
exactly one new summary per invocation, so this stop could not reuse the first
stop's record.

## Preservation and limits

The original stand's bot and real Loom retained their container IDs, start times
and images/configuration as checked by the harness. All original `shuttle_%`
table fingerprints remained identical. The three newly created containers and
temporary PostgreSQL volume were removed. PostgreSQL and the application exited
0 without OOM. The protocol peer required Docker's cleanup kill and exited 137
without OOM after application measurements; that cleanup is not application
shutdown evidence.

This proves subsecond observed termination and same-task recovery for this one
context-cancellable blocked HTTP scenario. It does not force the hard watchdog,
prove universal worst-case termination, exercise an actual Loom/Zitadel outage,
terminate arbitrary child processes, or complete the task's business action and
Telegram delivery. The synthetic run deliberately remained running. The one-second
setting is the concurrent drain budget; deferred telemetry/database cleanup is
outside the drain measurement and under the separate five-second watchdog.

Ignored native evidence is
`.cache/shuttle-live-test/shutdown-fault-qa/checkpoint.json` and its two stop logs.
Harness hashes and the progress checkpoint are under
`.cache/shuttle-work/shutdown-fault-qa`. No commit, push, production deployment or
Cloud change was made. Broader [shutdown diagnostics](shuttle-shutdown-diagnostics.md)
and [watchdog subprocess limits](shuttle-watchdog-qa-2026-10-15.md) still apply.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
