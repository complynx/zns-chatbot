# Real Telegram native specialist access — 2026-10-18

Status: PASS for native administrator grant/revoke/restore, roleless no-pass
specialist reads, one Telegram-initiated Loom read and same-container restart.
This is scoped acceptance on the owned schema48 / `mvp-54` stand, not complete
administrative agent/mixed handoff, unattended-model behavior, live-controller
rollback or production readiness.

## Setup and native administration

All Telegram UI actions used the existing account and `@complynx_test_bot` chat.
A distinct `QA staff access - SYNTHETIC` event uses Europe/Amsterdam, with two
specialist aliases and two synthetic bookings on 20 October. SQL seeded only
fixture event/schedule/preparation/booking data; it did not insert a specialist
binding or its operation receipts. The current account has no pass in this event.
Money and bookings were not mutated by the tested access/read workflows.

The account initially had no local IAM grants. Two temporary `zinusya.admin`
project grants enabled native administrator QA; their initial state, intent and
exact IDs were recorded. Only those two grants were later removed, and the local
IAM query confirmed the original empty state. This uses the owned IAM's
[management role-assignment API](https://zitadel.com/docs/reference/api/management/zitadel.management.v1.ManagementService.ListUserGrants),
not a production permission change or another Telegram account.

The actual Mini App opened inside Telegram Web and signed in. Its administrative
form resolved the current Telegram account, prepared an exact Alex grant and
showed event, target, action and event-local expiry. Preparation alone left zero
binding effects. Native confirmation created version1. Native revoke created
version2 and removed the event from the specialist list. A further exact native
grant restored version3. Each action retained its own canonical receipt. Binding
and receipt rows were verified through SELECTs, not created by the QA harness.
The real Loom run count stayed 122 throughout these native actions: no agent
turn was needed to perform them.

## Specialist without a pass or global role

After deleting the temporary admin grants, the real form hid and cleared the
administrative region. The specialist event stayed available through its private
Alex binding. The whole timetable displayed both non-cancelled fixture bookings;
My clients displayed one Alex booking. Own detail exposed its synthetic contact
`tg://user?id=900000000002`; this link was inspected, not followed. The fixture
client has no display name, so this does not establish real client-name rendering.
The foreign Beth booking exposed no contact/detail control.

Fresh delegated IAM API reads agreed with the UI: two timetable rows, one own
client/contact, foreign client detail404 and protected administrative settings403.
Administrative discovery correctly returned HTTP200 with no methods. Dates and
prices used Europe/Amsterdam with GMT+02:00 and 10.00 EUR. These are synthetic
booking prices, not confirmed payments.

One ordinary Russian read request was then sent through the actual Telegram
composer. The bot created exactly one task and one Loom run. A manual counted
provider drove Loom's real script broker and `zinusya_massage_staff` timetable and
client tools under that roleless user. Their actual results matched both booking
IDs, own alias/contact and timezone before the final response was supplied.
The bot delivered one Russian reply; the outbox receipt was Telegram message4551.
The reply was also observed in Telegram Web. No second message/request/run was
created when the first exact-text selector timed out on Telegram's nonbreaking
spaces around formatted times.

Manual provider responses prove transport, authorization and tool effects; they
do not prove unattended model interpretation or general language behavior.
No administrative agent write or form/agent approval handoff was exercised here.

## Restart and cleanup

After that task completed, the same bot container was stopped with a five-second
grace limit and started again. Stop took 0.766 seconds, exit0, without OOM. The
poller, eight Compose companions and counted provider remained unchanged. Full
binding, operation, alias and booking rows matched before and after restart.
The real Mini App reopened and signed in again: administrator region hidden,
private event present, own-client detail/contact still usable. This does not
claim the browser reused the identical bearer/session rather than logging in again.

All ten primary Compose services are running; no new container was needed for
this QA. The reusable mock stacks remain stopped. No temporary admin grant remains.
The active version3 binding and two synthetic bookings are retained for subsequent
mixed-flow QA. The two SQL-only fixture preparations were explicitly expired so
they do not appear as pending user choices; their bookings were left intact.
No task remains unfinished. No production change or Git publication occurred.

## Preserved harness failures and evidence

The first fixture preparation assumed a flat JWT roles claim and stopped before
writes; actual delegated administration discovery verified access instead. Fixture
inserts committed once, then a guessed pass `event_id` column broke the post-check;
read-only reconciliation used passes.registration_id and did not replay inserts.
The read gate initially expected403 for discovery, which intentionally returns200
with no methods; protected settings403 is the relevant denial. The browser's
strict full-text selector timed out on nonbreaking-space formatting; its delivered
receipt and visible text were reconciled without resend. Fixture-preview expiry
committed once before a multiline JSON parser failed; a read-only compact JSON
query verified both exact expired rows without repeating the update. These were
QA-harness assumptions, not patched product behavior.

Ignored evidence: `.cache/shuttle-work/massage-staff-real-v1-qa/`, especially
`checkpoint.json`, `native-grant-revoke-restore.json`, `roleless-staff-reads.json`,
`telegram-loom-read.json`, `telegram-loom-delivery.private.json`, `restart.json`,
`iam-before.json`, `iam-changes.json` and `fixture-driver-history.txt`.

Next: administrative agent-only writes and both form/agent continuation directions
with exact canonical consent and original receipts. Complete live-controller
rollback and unattended-model gates remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
