# Existing-run overlapping IAM/Loom outage QA — 2026-10-18

Accepted on a separate disposable `zinusya-mock-existing-run-outage` stand using
schema49 / `mvp-56` and frozen Loom fc6c574. A real original task, run and delegated
session survived overlapping IAM/Loom downtime and a cold bot restart. After the
specified restoration sequence, the bot automatically resumed the same shutdown-
paused run and delivered one final message. No operator Resume or replacement run
was used. This gate includes a deliberate bot restoration barrier, described below.

## Boundary and interruption

The nine resource-limited services had independent databases, IAM organization, CA,
fake bot token and provider mailbox, with pinned local images. The initial state
had no users/tasks/runs/provider requests/outbox or sends. One synthetic update7101
from501 entered the authenticated real webhook and returned202. Actual IAM and
Loom created one task, identity, auth session and run. No SQL fixture created or
changed task, run, session, queue, delivery or billing state.

The driver jointly observed the original task/run/session IDs and an unresolved
provider request before interruption. No model response, domain write or Telegram
send had occurred. The bot then stopped, followed by Loom. The real Loom engine
persisted the original run as `paused` with reason `shutdown` and recorded one
platform-sponsored unknown inference attempt. Its model step became completed
with ErrorCode=platform_paid_unknown, preserving the original client request ID.
The actual credit ledger retained the matching release of the user's reserved
exposure. All credit and usage values here are synthetic fixture accounting;
this is not evidence of a real provider charge or financial settlement.

Provider and IAM then stopped, so both IAM and Loom were unavailable simultaneously.
The same bot container started cold. It reached readiness and persisted the same
task in `awaiting_authorization`, retaining the original immutable request,
run ID and session ID. The original Loom run stayed at its recorded shutdown
checkpoint, with no outbox, domain write or send.

## Explicit restoration sequence

The bot was stopped again at a fixture barrier before restoring dependencies.
The exact same IAM, Loom and provider containers were then started in that order.
The provider shares Loom's network namespace; its stop/start allowed it to join
the restarted Loom namespace. Readiness plus an authenticated count-only request
checked provider availability before the bot was released. The count request did
not create another inference or provider response. The shutdown run remained
unchanged while the bot was stopped.

Starting that same bot allowed its typed paused/shutdown recovery path to resume
the original run automatically. One new inference request appeared in the original
run; the first request remained unanswered. A manual final response was published
only to the exact new request. Actual Loom execution then completed the same run,
and the original task/session delivered “Продолжаю.” to501 once.

Final checks found one user, inbox row, task, auth session, run, delivered outbox
row and sendMessage attempt. The final receipt matched that message/chat. There
were exactly two model steps: the original sponsored unknown and the successful
new attempt, with one platform attempt record and no business write_operation.
Other than the intentionally restarted bot, IAM, Loom and provider, five companion
services retained their IDs and start epochs. Every service retained its container
ID; no OOM occurred.

## Shutdown timing and review

The two bot stop observations were1.016 and0.969 seconds. These measurements include
Docker stop and the following Docker inspect; they are not application drain times.
The corresponding structured application diagnostics reported completed drain in
2.839332 and3.016979 milliseconds with a1000ms budget. Public/private HTTP and workers
all completed. These observations do not prove the whole-bot worst-case watchdog.
Loom's corresponding stop observation was0.844 seconds, exit0.

The fixture Python provider required Docker's five-second stop timeout and exited
137, with OOMKilled=false. This was a forced fixture-process termination, not a bot
or Loom OOM, and remains recorded. Its lifecycle is not a provider graceful-shutdown
acceptance claim.

Independent correctness and safety reviews of the complete driver were clean.
Python syntax passed, and21 source/module/configuration/frozen-contract hashes were
pinned before effects. No initial runtime assertion failed in the outage scenario.

## Preservation investigation and cleanup

The first primary-stand preservation assertion failed: river_job changed in
addition to River bookkeeping. The primary bot log showed JobCleaner deleting two
completed jobs at09:57:31 UTC. During investigation, two more completed jobs aged
past24 hours and were removed at10:05:31 and10:07:01. A first reconstruction using
only the initial pair therefore also failed; both failures remain retained.

The cause was proved without changing primary or fenced databases. pg_restore
extracted only the prior retained archive's River data to stdout; the original
archive SHA256 was reverified. One MVCC SELECT combined current jobs with exact
archived missing completed rows whose24-hour retention elapsed after the baseline.
It reproduced the original full-table JSONB fingerprint exactly, including every
remaining row and field. The four deleted job IDs were239,237,238 and236. No broad
river_job exception was accepted. Among the112 recorded fingerprints, the only other differences
were River leader/queue bookkeeping; business tables, bot/poller fingerprints and
primary Loom run count were unchanged. Task/outbox quiet checks passed.

Private accepted application and Loom archives are298683 and91612 bytes, with
SHA256 values retained and rechecked. The original manifest, exact task/run/session,
steps, accounting evidence, provider mailbox, lifecycle diagnostics, preservation
failures and reconstruction proof remain ignored local artifacts. These archives
are not a new restore or IAM backup acceptance claim.

All nine services were stopped, inspected for ownership/no OOM, and removed with
this project's volumes and network. Owner-label and Compose-project checks found
no remaining resources. Generic ignored fixture configuration/credentials remain.
Production and the Loom developer's checkout/stands were untouched. Nothing was
staged, committed, pushed, merged or deployed to production.

## Limits and next gate

Telegram was simulated and inference was manually supplied; bot, IAM, Loom and
persistence were real. This acceptance is one restoration order with a deliberate
bot barrier for provider namespace readiness. Reverse restoration order, continuous
bot operation throughout dependency return, expired delegated-session renewal,
uncertain domain tools/sends and real Telegram rendering remain separate gates.
The subsequent [reverse-order continuous-bot gate](shuttle-existing-run-reverse-outage-qa-2026-10-18.md)
now covers Loom/provider-first readiness with IAM still down and no second bot
stop during dependency return. IAM-first return with a continuous bot remains open.
D16's untyped module_discovery_unavailable pause is still not automatically resumable.
The [cold task gate](shuttle-bot-cold-iam-restart-qa-2026-10-18.md) and
[credential expiry gate](shuttle-iam-credential-expiry-qa-2026-10-18.md) remain
separate evidence. No production-readiness claim follows from these results.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
