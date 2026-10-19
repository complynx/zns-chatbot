# Real specialist notification delivery QA — 2026-10-18

Accepted on our owned `shuttle-live-test`, schema49 / `mvp-56`: a new booking and
its cancellation each produced one durable notification delivered to the authorized
current account in `@complynx_test_bot`. Both messages were then visible in Telegram
Web. No production or Loom developer stand was changed.

## Scope and preparation

The existing event `QA massage schedule - SYNTHETIC` has timezone `Europe/Berlin`,
event version3 and schedule version1. The current account already had an assigned
pass; every booking in this event belonged to it. There was no specialist binding
for this event and no specialist notification intent anywhere in the stand.
The current global preference was enabled/version4 after the
[mixed preference gate](shuttle-massage-specialist-preferences-runtime-qa-2026-10-18.md).

A separately recorded temporary IAM cycle allowed an exact prepared API operation
to bind specialist `alex` to that same account at version1. The grants were removed
before booking. Activation produced no notifications for old bookings.

This gate invokes the real delegated agent API directly. It does not run a Loom
agent or test native/browser booking controls. Service/IAM checks and the actual
background delivery worker are real. Synthetic event/service prices are not payments.

## Booking and cancellation

Live availability exposed both repeated02:00 instants across the DST transition.
The selected UTC instant `2026-10-25T01:00:00Z` is the second02:00, offset `+01:00`.
The first review caught a QA-only comparison of timezone strings; it was corrected
to compare instants and prepare canonical UTC before any fixture effect.
A fresh review then found no actionable findings.

The ordinary booking used exact prepare/confirm consent and a saved operation ID.
New booking `574c8c48-18cd-45b8-b597-db5c07e59568` produced one `booked` intent
with binding version1 and preference version4. The worker delivered Telegram
message4600 to chat8968148043. The payload contained the event-local start
`2026-10-25T02:00:00+01:00`, end02:15 with the same offset, explicit IANA timezone,
45.00 BYN, booking reference, specialist/client names and one contact entity pointing
to the authorized account.

After observing that delivery, a distinct prepared operation cancelled only this
new booking at version2. One `cancelled` intent was delivered as Telegram
message4601 to the same chat. The previous delivered receipt remained delivered;
the cancellation payload had no contact entity. Owner reminders for the new booking
ended blocked, with no real owner reminder sent.

Telegram Web in the authorized chat showed both headings, reference, price,
timezone/offset and the timetable buttons. The browser's message-container IDs461
and462 differ from Bot API message IDs4600 and4601; they are not interchangeable.
No timetable button was clicked and no Mini App browser acceptance is inferred.
No screenshot file is claimed.

## Cleanup and preservation

A second tracked IAM cycle revoked the new specialist binding at version2, then
removed both temporary grants. The original absence now has an auditable revoked
binding; no audit rows were deleted. The new fixture booking remains cancelled.
All preexisting booking rows are byte-equivalent at the JSON readback level. The
global preference remains enabled/version4. Forty-one protected table fingerprints
for orders, passes, registrations, payments, pairs and settings match baseline.
Exactly two specialist intents exist, both delivered to the current account; there
was no extra Loom run and the durable task/outbox quiet check passed.

No containers were created or restarted, no new session credentials were written,
and no SQL business mutations were used. Each operation ID was saved before its
request; uncertain acknowledgment stops the helper instead of replacing an effect.
Correctness review required two passes; the independent safety review was clean.
The Git index remains empty and nothing was published.

Ignored evidence:
`.cache/shuttle-work/specialist-notice-real-delivery-v1-qa/` contains the accepted
checkpoint, source gate, independent setup/cleanup IAM journals and read-only
`browser-observation.json`. The checkpoint retains prepared operations, actual
payloads/receipts and baseline preservation data.

Running-worker mute/re-enable and revoke/regrant generation fences remain open,
as do pending/inflight/unknown process restart and full live-controller rollback.
This happy path does not prove unknown-send recovery, retry/reconcile administration,
unattended model behavior or Mini App controls. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
