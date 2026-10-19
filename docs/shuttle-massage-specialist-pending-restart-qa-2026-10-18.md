# Never-attempted specialist notification restart QA — 2026-10-18

Accepted on the disposable `zinusya-mock-notice-pending` stand with schema49 /
`mvp-56`. A real native booking created a pending notification and River job.
Both survived a graceful restart of the same bot container; after queue resume,
the original job delivered exactly one message to mock Telegram.

## Setup and queue control

The stand used nine resource-limited services, pinned local bot/Loom/file-worker
images, an independent database and IAM organization, CA and fake bot token.
Synthetic users501/502 opened native sessions using signed mock initData and real
IAM. SQL seeded only the initial event, schedule, assigned pass and specialist
binding. Native prepare/confirm endpoints created and cancelled the booking.
Notification delivery states and River job states were not written by SQL.

A small Linux helper used the pinned River v0.49.0 public QueuePause, QueueResume
and QueueGet APIs. It checked the fixed fake token, exact stand service marker,
local database host/name/schema and a15-second context. Each SDK job used the
candidate image, read-only files/root, user10001, dropped capabilities, no new
privileges,96MiB memory/swap and32 PIDs. Both jobs exited0 without OOM.

An initial quiet bot stop took0.719 seconds. While it was stopped, the helper
paused the default queue. The same container then started into the persisted
pause. This setup barrier prevented a worker from claiming a task before the
native booking could be observed; it is separate from the tested pending restart.

## Observed behavior

The native booking produced one notification in `pending`, version1, with no
attempt UUID, no frozen payload and an empty receipt. Its original River job was
`available`, attempt0, with no attempted_at. Mock Telegram had zero attempts and
zero accepted messages. The queue remained paused.

The bot stopped gracefully in0.766 seconds, exit0. While stopped, the original
notification and selected River job fields were unchanged. Starting the same
container preserved both records and still produced no send. All eight companion
services retained their IDs and start epochs throughout both bot restarts.

After the second bounded SDK job resumed the queue, that same River job ID
completed at attempt1 with unchanged arguments. The original notification became
`delivered`, version3, with an attempt UUID and receipt matching the single accepted
mock message to501. There was exactly one sendMessage attempt and one accepted
effect at this milestone.

Native cancellation then produced its own delivered notice without changing the
original booked receipt. Final mock state contained two attempts and two messages
(booked and cancelled), no active booking, only blocked owner reminders, and no
new Loom run. Both native sessions were logged out and their temporary file removed.
No model inference or real Telegram send was requested.

## Review, cleanup and scope

Independent correctness and safety reviews found no actionable findings. Before
execution, the source gate pinned eleven files: driver, helper source/executable,
Go module files, stand/mock helpers and the exact mounted configuration/manifest.
The SDK executable was built with the repository's Go1.27.2 toolchain.

The accepted database was exported as a300609-byte custom archive; its SHA256 was
retained and rechecked. The original manifest, mock transcript, exact notification
and job snapshots, SDK outcomes and terminal container identities remain ignored
local evidence. This archive is not a new restore acceptance claim.

The two SDK containers and nine stand services were removed after ownership and
terminal-state checks. No OOM was recorded. Both owner-label and Compose-project
checks confirmed no remaining containers, volumes or network. Generic ignored
fixture configuration/credentials remain, as designed by the stand tool.

The primary stand retained the same bot/poller fingerprints and Loom run count.
All112 raw fingerprints matched except River leader/queue bookkeeping; task/outbox
quiet checks passed. Production and the Loom developer's stand were untouched.

This gate proves recovery of an existing, never-attempted notification/job under
persisted operator queue pause. It does not prove recreation of a missing job,
unpaused scheduling races, broader task/outbox restart, combined IAM/Loom outage,
Mini App browser controls or complete live-controller rollback. The earlier
[retryable/inflight gate](shuttle-massage-specialist-restart-qa-2026-10-18.md) and
[generation fence gate](shuttle-massage-specialist-generation-qa-2026-10-18.md)
remain separate evidence. No commit, push, production change or merge was made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
