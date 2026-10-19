# Reverse dependency restoration with a continuous bot — 2026-10-18

Accepted on the disposable `zinusya-mock-existing-run-reverse` stand, schema49 /
`mvp-56` with frozen Loom fc6c574. Loom and its provider became ready while IAM
remained stopped. The same cold bot process kept waiting, then automatically
resumed its original run after IAM returned last. One final message was delivered.
There was no second bot stop or restoration barrier before completion.

## Actual path

The nine resource-limited services used independent databases, IAM organization,
CA, fake bot credentials and provider mailbox, with pinned local images. The stand
began empty. One synthetic update7201 from501 entered the authenticated real
webhook and returned202. The bot created its actual identity/task/delegated session
and Loom run. No SQL fixture created or changed task, identity, run, session,
queue, delivery or accounting states.

The driver jointly observed the original task/run/session IDs and an unanswered
provider request, with no domain write, outbox or send. The bot stopped once;
Loom then stopped and persisted that original run as `paused/shutdown`. The
interrupted inference became one sponsored unknown attempt, preserving its client
request ID and recording a matching release in the synthetic credit ledger. The
provider sidecar and IAM stopped next, leaving both IAM and Loom unavailable.

Starting the same bot container cold produced readiness and the original task in
`awaiting_authorization`. Its immutable request, run ID and session ID remained
unchanged. The driver fixed that process's StartedAt and asserted it on every
subsequent running-state guard through final delivery.

The same Loom container was started first, then its provider rejoined Loom's
network namespace. While IAM was still stopped, Loom readiness returned200 and
an authenticated count-only provider request succeeded. The bot remained running
and awaiting authorization; the original run stayed at its shutdown checkpoint.
The count probe created no inference request or answer.

The same IAM container was started last. Without another bot restart, operator
Resume or replacement run, the bot's typed shutdown recovery resumed the original
run. One new inference request appeared. Only that exact new request received a
manual QA response; the first request remained unanswered.

The same task/run/session then completed with one delivered outbox row and one
sendMessage attempt: “Продолжаю.” to501, with matching message/chat receipt. Final
checks found one user, inbox row, task, run and auth session, no business write,
and exactly two model steps: one sponsored unknown and one successful response.
Five companion services other than the intentionally restarted bot/IAM/Loom/provider
retained their start epochs; all nine retained their container IDs. The bot's
cold-process epoch matched its final epoch, proving continuity across dependency
return. Cleanup stopped it only after this acceptance checkpoint.

## Timing, review and cleanup

The initial bot stop observation took0.969 seconds, including Docker stop and its
following inspect. Its structured application drain completed in2.04978ms with a
1000ms budget; public/private HTTP and workers all completed. Loom's stop
observation took0.828 seconds, exit0. These are scoped observations, not a complete
worst-case shutdown/watchdog proof.

The fixture provider required Docker's five-second stop timeout and exited137,
with OOMKilled=false. That known fixture lifecycle limitation remains recorded;
it is not a bot/Loom OOM or a graceful-provider-shutdown claim.

Independent correctness and safety reviews of the complete driver were clean.
Python syntax passed and21 source/module/configuration/frozen-contract hashes
were pinned before effects. No runtime acceptance assertion failed in this gate.

Private accepted application/Loom archives are298659 and91565 bytes; their SHA256
values were retained and rechecked. Lifecycle checkpoints, the original manifest,
exact task/run/session, model steps, synthetic accounting, private provider files,
mock transcript and terminal container identities remain ignored evidence. These
archives are not a new restore or IAM backup acceptance claim.

All nine services were stopped, checked for ownership/no OOM, and removed with
this project's volumes and network. Both owner-label and Compose-project queries
confirmed no remaining runtime resources. Generic ignored fixture credentials and
configuration remain for diagnosis.

The primary comparison matched112 raw fingerprints except River leader bookkeeping.
Its bot/poller fingerprints and Loom run count were unchanged; task/outbox quiet
checks passed. Full River job rows were retained before and after: all46 rows and
every field matched, with no inserted or deleted ID. This avoids treating arbitrary
river_job changes as acceptable maintenance. The earlier
[retention investigation](shuttle-existing-run-outage-qa-2026-10-18.md) and its
initial failed assertions remain preserved separately.

Production and the Loom developer's checkout/stands were untouched. Nothing was
staged, committed, pushed, merged or deployed to production.

## Scope

Bot, IAM, delegated authorization, persistence and Loom execution were real.
Telegram was simulated and inference manually supplied; this does not prove model
quality, a domain tool operation or actual Telegram rendering. Credit/usage values
are synthetic, not evidence of a real provider charge or financial settlement.

Together with the separate IAM-first restoration gate, this establishes two
specified restoration paths. IAM-first return with the bot continuously running,
expired delegated-session renewal, abrupt Loom kill and uncertain domain writes
or sends remain open. D16's untyped module_discovery_unavailable pause is still not
automatically resumable. No production-readiness claim follows.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
