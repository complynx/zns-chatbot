# Massage owned runtime and Telegram QA, 2026-10-17

The massage rollout below used schema46 / manifest `mvp-49` with image
`sha256:30995ceb55f09c853105344188830b8ff7ddd57ebd5226af30305909e3579b81`.
The owned runtime now uses the later [readable-approval image](shuttle-massage-readable-approval-qa-2026-10-17.md)
`sha256:904344b47e5852efcacd1020911fcad25ec57969d82a3f6c9a3d9f3755f6adca`,
with the same schema46 / `mvp-49`.
This accepts the rollout, native owner lifecycle, a pure-agent booking and two
mixed happy-path samples with manual inference. The pending native cancellation
also survived an actual bot restart. Full critical-path and unattended-model
acceptance remain open. No production change or readiness is claimed.

## Build, migration and activation

The unmodified release Dockerfile built the reviewed source. The temporary
builder and cache were removed. A private main-database backup was restored into
an isolated PostgreSQL instance; migration 45 to 46 preserved every old table
hash and added the three empty massage preparation/booking/operation tables.
The restored-copy database and migrator were removed after verification.

Main activation retained existing configuration, credentials, durable data and
the same Telegram poller. Eight companion containers retained their identities,
images and starts. Bot shutdown took 0.657 seconds, with exit 0 and no OOM.

Two initial preservation guards failed and remain recorded. The awaiting-task
monitor legitimately advanced technical lease/version fields. Startup also
advanced River scheduling metadata for that task. Read-back against the private
backup found no added, removed or changed job identities or arguments; logical
task state and business records were preserved. Revised guards permit only the
observed technical fields. They do not exclude business state from comparison.

## Real Telegram observations

Only the authorized account and `@complynx_test_bot` chat were used, with actual
Telegram, Zitadel and Loom. The provider is a manually serviced synthetic fixture.
Token counts are synthetic scenario units, not real tokenizer or billing proof.
These runs do not establish unattended model behavior.

- Native only: `/massage` opened the signed-in embedded form. Preview did not
  book. Separate confirmation booked one slot; separate cancellation confirmation
  cancelled it. There were no additional tasks or Loom runs during that form
  lifecycle. Price was 45.00 BYN; no payment or refund was asserted.
- Form to agent, first attempt: the saved proposal expired before resolution.
  Loom rejected it with `approval_expired`; no booking resulted. Old consent was
  not reused.
- Form to agent, fresh attempt: Loom adopted the same native proposal under its
  new operation ID, preserved its original expiry, and booked once after fresh
  consent. The form displayed the booking at 02:00 GMT+01:00 on 25 October,
  Europe/Berlin. However, the final model response was not supplied during manual
  QA; Loom ended with `active_time_expired` and Telegram showed a generic task
  failure after the effect. This is a failed complete dialogue, not a happy-path
  pass. An independent provider timeout has not been established.
- Agent to form: the agent prepared cancellation of that booking. The form's
  unfinished-choice list found the same proposal and showed its original expiry.
  Separate native confirmation cancelled the booking. On a new user request, the
  agent read the current cancelled record and exact native operation receipt,
  rejected only its now-unneeded pending action, and delivered a Russian final
  answer. The run and steering task completed. Database read-back found exactly
  one book and one cancel operation for this booking, with no duplicate effect.
  One manually authored read request incorrectly included an undeclared `kind`
  field and was rejected as `tool_failed`; the corrected declared schema worked.

A later fresh form-to-agent run completed successfully. It used a newly saved
native proposal, a new Loom run and fresh explicit consent. Exactly one agent
booking receipt consumed that same proposal before its original expiry; the
native and Loom operation IDs differed. Both the initial task and steering task
completed, and Telegram delivered the Russian final answer. The form then showed
the booked record. This later pass does not replace either earlier failure.

Pure-agent booking also completed without opening or selecting a slot in the
form. The agent read the current schedule, read live availability separately,
prepared a new agent-origin proposal for 03:00 GMT+01:00, and waited for explicit
text consent. One booking resulted and Telegram delivered the Russian final
answer. Both tasks completed. This proves booking in that scenario, not all
agent cancellation or specialist lifecycle behavior.

## Pending cancellation across a bot restart

After the fresh mixed booking, the actual embedded form prepared cancellation
but did not apply it. Only the bot container was stopped with a five-second
termination limit and started again. Shutdown took 0.687 seconds, exit 0, no OOM.
Container/image identity remained the same; all nine Compose companions retained
their IDs, images, starts and restart counts. All rows in the three massage
tables retained identical hashes across the restart. Readiness returned.

The still-open form then applied the same saved proposal and operation, before
the original expiry. It displayed Cancelled. Exactly one cancellation receipt
was stored. No new confirmation proposal or renewed expiry was used. This does
not test a lost HTTP response or worker kill during an uncertain apply.

The initial read-only probe incorrectly assumed every massage table had an `id`
column. It failed before any runtime mutation or restart; the corrected probe
orders canonical row JSON. That harness error remains in the local checkpoint.

The selected repeated DST hour was 02:00 GMT+01:00, distinct from 02:00 GMT+02:00.
The form rendered the offset and Europe/Berlin zone explicitly.

The generic post-effect failure notice now has a [reviewed and installed fix](shuttle-task-failure-notice-qa-2026-10-17.md).
An actual English Telegram notice passed with controlled time-counter injection
and unchanged prior booking. This does not change the historical failed dialogue
or establish a new real after-effect business scenario.

## Remaining gates

The later [native booking response-loss/clean-restart gate](shuttle-massage-response-loss-real-qa-2026-10-17.md)
passes. The later [real Mini App precommit cancellation SIGKILL gate](shuttle-massage-hardkill-real-qa-2026-10-17.md)
also passes. Agent/mixed lost-response handoffs, other full-bot kill boundaries,
remaining agent cancellation paths and broader service-outage recovery remain open.
[Readable canonical approval](shuttle-massage-readable-approval-qa-2026-10-17.md)
also has scoped acceptance: the service quote follows the saved bot interface
language and no longer appends raw argument JSON. Earlier observed messages remain
historical evidence. The generic failed-task notice has the scoped acceptance
linked above. Specialist lifecycle, notifications and full Python massage parity
remain separate unfinished work.

Local ignored evidence is under `.cache/shuttle-work/massage-images-v1-qa/`,
`massage-schema-v1-qa/`, and `.cache/shuttle-live-test/massage-main-cutover-v1/`
and `massage-real-qa-v1/`. Evidence contains checkpoints; the private database
backup must not be published. Screenshot observations were displayed by the
browser tool; no saved screenshot artifact is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
