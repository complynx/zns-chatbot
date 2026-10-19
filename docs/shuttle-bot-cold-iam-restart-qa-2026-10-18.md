# Actual bot cold-start IAM outage and task restart QA — 2026-10-18

Accepted on a disposable `zinusya-mock-bot-auth-restart` stand with the current
schema49 / `mvp-56` bot. A real webhook accepted a message while IAM was stopped
and the bot had a fresh in-memory cache. The same unfinished task survived another
bot restart, then completed once through real IAM and Loom after IAM restoration.
Replaying the original update produced no new task, run or delivery.

## Stand and input

The nine-service stand used pinned local bot/Loom/file-worker images, independent
PostgreSQL/IAM/CA/fake bot credentials, and explicit resource limits. It began with
zero users, tasks, outbox rows, Loom runs, provider requests or Telegram attempts.
No task, identity, domain, queue or delivery state was created through SQL. SQL was
used only for observation. The test required no native Mini App session or
administrative user grant.

The quiet bot first stopped in0.735 seconds, exit0. The exact owned IAM then
stopped. The same bot container started into a fresh process while IAM remained
unavailable; readiness returned200 without requiring IAM. This setup barrier
establishes an empty credential cache before intake.

A single synthetic private update7001 from user501 requested the reply “Готово.”.
The mock forwarded it through the authenticated real application webhook, which
returned202. The actual intake created one user, conversation, inbox row and task.

## Pending work across restart

The worker persisted the task as `awaiting_authorization`. The user had no bound
IAM subject. The task had no auth session or run, no bootstrap attempt, and no
outbox row, provider request or Telegram send attempt existed.

The driver compared the immutable task identity, conversation, sequence, creation
time and normalized request. The inbox payload matched that normalized request;
its nested raw_update matched the exact original Telegram update. It retained the
actual before-stop task and inbox evidence, rather than seeding expected states.

With IAM still stopped, the same bot container terminated gracefully in0.671
seconds, exit0. The same pending task was observed while it was stopped. Restarting
that same container again produced a ready application with IAM still unavailable.
The original task remained `awaiting_authorization`, with the same immutable
request/inbox, unbound user, and no run, session, outbox or send.

## Recovery and replay

The same IAM container was started and its discovery readiness checked. The
original bot task then automatically provisioned and bound the synthetic identity,
obtained delegated authorization, and reached one real Loom auth session and run.
The exact pending provider request contained the original requested reply.

One manual QA response was published atomically to that request. Actual Loom
execution completed and the bot's original task reached `completed`, retaining its
run/session IDs. One outbox row became `delivered`; its receipt matched the only
mock sendMessage result, to501 with the text “Готово.”. The final database check
confirmed exactly one Loom auth_sessions row as well as one run. The actual model
request contained the Zinusya name; this is an inspection of the request, not a
separate complete prompt-contract acceptance.

The identical original update was replayed through the mock webhook and returned
202. After a three-second observation interval, the stand still had one user,
inbox row, task, run, session, provider request and outbox row. The delivered
outbox/receipt, accepted bot message and send-attempt transcript were unchanged.

All seven companions other than the intentionally restarted bot and IAM retained
their IDs and start epochs. Both restarted services retained their container IDs.
There was no OOM and no manual operator Resume or replacement run.

## Review, evidence and cleanup

Independent correctness review found that the first QA driver compared the inbox
payload directly to the raw Telegram update. The application stores a normalized
intent envelope containing raw_update. Source inspection confirmed this contract;
the assertion was fixed before effects. A fresh no-history correctness review and
independent safety review were clean. Driver syntax, UTF-8/LF checkpoint round-trip
and immutable-request selection checks passed. Seventeen source/config/module
hashes were pinned before runtime effects.

The accepted application database archive is298669 bytes; its SHA256 was retained
and rechecked. The original manifest, source gate, lifecycle checkpoints, task and
outbox snapshots, private provider files, mock transcript and terminal container
identities remain ignored local evidence. The archive is not a new restore or IAM
backup acceptance claim.

All nine services were stopped and checked for ownership/no OOM, then their
containers, volumes and network were removed. Both owner-label and Compose-project
queries confirmed no remaining runtime resources. Generic ignored fixture files
and credentials remain for evidence; no new primary-stand session was created.

A fresh primary-stand comparison matched112 raw fingerprints except River
leader/queue bookkeeping. Its bot/poller fingerprints and Loom run count were
unchanged, and task/outbox quiet checks passed. Production and the Loom developer's
stand were untouched. No commit, push, merge or production deployment was made.

## Scope

The bot, database, IAM, delegated authorization and Loom agent loop were real.
Telegram transport was simulated and model inference was manually supplied. This
proves cold intake and pending-task recovery, followed by a single ordinary final
reply; it does not prove model quality, a domain tool operation, actual Telegram
rendering, existing-session renewal during a running task, uncertain sends, or
combined IAM/Loom downtime after an existing run starts. Those gates remain open.
D16's untyped module_discovery_unavailable pause still cannot be automatically
resumed. The [client expiry gate](shuttle-iam-credential-expiry-qa-2026-10-18.md)
and [specialist notification restart gate](shuttle-massage-specialist-pending-restart-qa-2026-10-18.md)
remain separate evidence.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
