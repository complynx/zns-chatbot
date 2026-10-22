# Consumer approval tracing, 2026-10-22

The isolated real-IAM/Loom gate passed plain-text consent after graceful bot and
Loom restarts. The original approval produced one pending knowledge suggestion,
one backend receipt and one final delivery. It did not publish an article.
The successful commit request itself belongs to the consumed-consent trace.

## Installed fixture

Only the owned `iam420-zinusya-v1` synthetic-Telegram fixture was changed.
Bot source `be55b4a1`, image `7e0fc10b`, adds the accepted
[private HTTP route templates](shuttle-http-route-tracing-2026-10-22.md).
It retains `mvp-92/schema72`. Real Zitadel 4.20.0 and published Loom `b2b8d429`
image `9ddd3175` were used. Model replies came from the controlled mailbox
provider; no paid model or real Telegram authorization was requested.

Before installing the bot image, full custom-format bot and Loom database
archives were saved and decoded. Counts and canonical row hashes for nine
user/profile/task/inbox/outbox and Loom run/step/input/provider-attempt tables
matched before and after startup. Only the bot container was replaced; eight
companion IDs and images, and schema72, were preserved. The installation ended
with all nine containers stopped. Its receipt is
`.cache/shuttle-work/http-route-fixture-install-20261022-v1/installation.json`.
The primary and separate browser IAM installations were unchanged.

## Accepted workflow and trace

1. An existing synthetic user asked to submit an exact general-knowledge article
   for independent moderation. Loom prepared the operation and requested consent.
   Title and complete body were delivered before the `Approve`/`Reject` reply
   keyboard. The labels contained no operation tags. The current generic approval
   still includes a JSON argument quote; this gate does not accept that UI as a
   finished human-readable knowledge approval.
2. The bot and then Loom stopped gracefully while approval was pending. Bot stop
   took 0.719 seconds; Loom stop took 0.594 seconds. Both exited0 without OOM.
   The exact original approval, operation and backend preparation, including its
   expiry, survived. Neither a replacement preparation nor a new approval was
   requested after restart.
3. The user sent ordinary Russian consent, without IDs or tags. The actual model
   input contained that steering input and the original pending action. The
   controlled provider resolved that exact input/approval/hash binding.
4. Exactly one original operation succeeded and produced one pending suggestion
   with the requested proposer, title, slug and body. Its backend receipt retained
   the original proposal. Article and revision counts stayed zero. The retained
   preparation from the earlier expired test was unchanged. One new run completed
   and one final response was delivered.
5. The bounded receiver exported173 spans. Origin execution and consumed-consent
   execution used different traces with exported causal links to their admissions.
   Exactly one bot `http.server` span met all commit conditions together:
   `POST /internal/agent/knowledge/suggest`, status200, the backend `completed_at`
   inside its interval, and exported parent ancestry to a Loom `execution_step`
   in the consent trace. Matching the timestamp in one span and ancestry in
   another was not sufficient.

All nine containers stopped with exit0 and no OOM. Both original configuration
files were restored byte-for-byte; cleanup reported no errors and removed the
gate's exact collector container. Evidence is under
`.cache/shuttle-work/consumer-approval-trace-20261022-v2/`, including
`checkpoint.json`, exported spans, container inspect records and private logs.
The final controller received independent review with no actionable findings.

## Retained failures and expiry boundary

The first controller stopped before consent because it expected a nonexistent
`doc.backend_operation_id`; the persisted contract uses `doc.operation_id`.
Its original receipt and prepared operation were retained. Continuation review
also found the collector's exclusive journal-create behavior and helper scratch
files that needed separate evidence directories. A subsequent guarded continuation
refused the remaining original validity window before starting any runtime effect.
No deadline was extended and the original operation was not replayed.

A separate attempt to cancel that expired request assumed the message would steer
the original run and timed out. Read-only diagnosis showed that the original run
had failed with `wait_expired`; the cancellation message had correctly become a
different run. A reviewed continuation answered only that existing follow-up model
request, without tools, and completed it. The original prepared operation stayed
unchanged, with no knowledge suggestion or backend operation. Both failed receipts
remain intact under `consumer-approval-trace-20261022-v1/`; the final readback is
`expired-cancel-finish.json`. This is not evidence that an expired approval was
successfully rejected through `interaction_resolve`.

## Remaining boundaries

This proves the controlled-provider runtime consent path, installed route
attribution and exported causal graph on the isolated fixture. It does not prove
live-model interpretation of arbitrary consent/cancellation, real Telegram UI,
all crash windows, existing-IAM database upgrade, external primary trace-backend
visibility or production readiness. Primary deployment remains separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
