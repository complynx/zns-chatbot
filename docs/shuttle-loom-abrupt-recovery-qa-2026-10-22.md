# Loom abrupt-exit recovery acceptance, 2026-10-22

One pending-inference recovery scenario passes on isolated `abrupt-loom-r2`,
using actual Zitadel, PostgreSQL/River and published Loom `34f46468` with
application source `4f03e5ff`, schema70 / manifest `mvp-91`. Telegram is mocked
and provider replies are controlled. This is separate from autonomous model,
real-Telegram and production acceptance.

## Observed recovery

One synthetic update was accepted once. It created one original user, inbox,
task, run and delegated session. Before the fault there was one pending model
step, one unanswered logical provider request, a live run lease and one running
River execution job. No module write, Telegram send or outbox item existed.

Only the owned Loom API/executor container received SIGKILL. It exited with code 137
without OOM. The same container was started once. All eight companion processes,
including the bot, provider and IAM, retained their identities and start epochs.
No new update, manual Resume, administrative recovery or lease/queue SQL repair
was supplied.

At the first recovery read, 16.429 seconds after Docker’s recorded container start,
the original run was executing again with a live lease and one new logical
provider request. This is an observation upper bound in this fixture, not an
exact recovery instant or scheduling/final-response SLA. Ordinary active-time,
lease and sponsored-attempt settings were retained.

The original inference remained unanswered. Its step completed as
`platform_paid_unknown`; one sponsored attempt retained positive unknown
exposure and the exact original step ID, run ID and request ID. Recovery used a
new logical request ID; the original inference was not counted as successful.

The controlled provider answered only the recovered new request. The original
task and run completed with the original auth session, without replacement.
One outbox item was delivered through one mock-Telegram attempt and one message;
stored chat/message IDs matched. Both completed model-step results were retained;
the original unknown-step result and sponsored exposure remained unchanged.
The primary stand was preserved.

## Review, archival and limits

Independent review strengthened two checks before execution: unknown exposure
must belong to the original model step/run; cleanup must match the complete
actual project container-ID set before fencing and before destruction. Both
controllers passed a fresh second review. No application change was needed.

Four database archives were fully decoded, digests verified and all nine service
logs retained. The nine owned temporary containers, their volumes and network
were removed. Ignored proof and reviewed controllers remain under
`.cache/shuttle-work/abrupt-loom-acceptance-20261022-v2/`. Original provider
requests remain under `.cache/shuttle-mock/abrupt-loom-r2/mailbox/`.

The [older failed observation](shuttle-loom-abrupt-exit-qa-2026-10-18.md), including
its expired lease and orphan job at 117 seconds, remains unchanged historical
evidence. [Loom #19](https://github.com/complynx/loom/issues/19) was already closed
for the merged implementation; this adds actual-IAM consumer evidence.

This does not test every kill window, queue backlog, exhausted recovery budget,
uncertain external write, authorization loss, pause/cancel race or OTel context
across the queue. SIGTERM with graceful shutdown remains the normal restart
path; SIGKILL is exceptional. The existing
[graceful recovery contract](shuttle-loom-resume.md) remains separate.
