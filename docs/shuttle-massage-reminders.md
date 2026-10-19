# Owner massage reminders

Date: 2026-10-17. Source component: schema47 / manifest `mvp-51`.
Owned runtime is schema47 / `mvp-51`. Installation, real-IAM authorization,
real Telegram delivery and real-Loom reminder-tool QA pass within the scopes below.

## Implemented storage contract

A new booking creates two owner reminder intents in the same transaction as the
booking, proposal consumption and operation receipt. Agent and Mini App use the
same insert helper. Original operation replay and proposal adoption retain the
original two intents; they do not create another pair.

Migration47 creates `shuttle_massage_reminders`, unique by booking and kind
`owner_long|owner_short`. It retains due time, independent retry time, state,
version, attempt, payload/receipt and creation time. Owner and event derive from
the canonical booking. A scoped internal reader joins the bot and organization;
it grants no participant or administrator authorization by itself.

Cancellation blocks pending/retryable reminders atomically with the booking and
receipt. It preserves their IDs and due times and advances their versions.
Inflight, delivered, unknown and other terminal delivery states are not rewritten.
An already reserved send may complete after cancellation; suppression before
reservation is checked again by the delivery worker.

Settings add `massage_notify_client_prior_long_seconds` and
`massage_notify_client_prior_seconds`. Omitted/zero means the legacy 3600/600
seconds respectively. Positive values use signed 32-bit seconds; this is the
storage/arithmetic bound, not a requirement that the long lead exceeds the short
one. Both are exposed through the existing versioned settings API and exact
approval/audit path. No secrets or startup parameters are added.

Changing effective leads atomically reschedules pending/retryable intents from
the canonical booking start and advances their versions. Unrelated settings do
not change them. `next_attempt_at` is a separate retry embargo and remains
unchanged. It initially uses database now, so an earlier due time does not wait
for an obsolete scheduling timestamp. Sent, unknown and inflight intents retain
their terms. Safe retry transitions rebase due time against current settings.

Booking holds settings SHARE through commit; settings CAS holds the conflicting
settings update lock. Multirow reminder mutations first lock UUIDs in ascending
order. Settings rescheduling does not lock event, owner or booking rows. Due times
are absolute PostgreSQL timestamps; internal read results normalize to UTC while
booking terms retain the event timezone snapshot.

## Verified store behavior

The final Linux Go 1.27.2/PostgreSQL 17 race gate passed 11 selected roots and
26 passing test events, with zero failures/skips. Tests cover both interfaces,
replay/adoption, deployment isolation, cancellation, rejected rollback with
retained history, booking/cancellation receipt failures, settings/audit rollback,
retry embargoes, untouched non-schedulable states and concurrent settings changes
with booking/cancellation. Settings approval descriptions remain within Loom's
4000-byte bound, and negative/overflow lead values are rejected.

Independent correctness and skeptical quality reviews, followed by fresh final
source reviews, found no actionable findings. Full configured lint passed before
the final one-line test correction; the final changed integration package also
passed configured lint with zero issues. Other source bytes remained unchanged.

The initial compile failed because a test referred to deployment fields on an
event value; the helper was corrected to use the bound profile scope. Native
compilation then succeeded, but integration cases skipped without a DSN and were
not counted as acceptance. The first real-PG gate passed eight roots and failed
UTC-vs-local `time.Time` structural comparisons of equal instants. A multiline
assertion was missed by the first correction, leaving one failed root in the
second gate, which otherwise passed ten. All three assertions now compare UTC
instants; the third gate passed. These were test assertion errors, not observed
incorrect due-time calculations. Earlier failures remain preserved.

Both failed runs and the accepted run retained their evidence; each one's exact
containers and private volume were removed. No test process was OOM-killed.
Limits were 2 GiB for Go, 512 MiB for PostgreSQL and no swap. The accepted freeze
and all ten main Compose fingerprints remained unchanged during the final gate.
Ignored evidence: `.cache/shuttle-work/massage-reminder-store-v1-qa/`,
`massage-reminder-store-v2-qa/` and `massage-reminder-store-v3-qa/`.

## Delivery component acceptance

The delivery worker is implemented and has component acceptance. The final Linux
Go 1.27.2/PostgreSQL 17 race gate passed 36 roots and 75 passing test events:
33 integration roots, one formatter root and two observability roots, with zero
failures/skips. Frozen source and all ten main Compose fingerprints remained
unchanged. The exact temporary containers and private volume were removed; no
process was OOM-killed. Limits were 2 GiB for Go and 512 MiB for PostgreSQL,
without swap. Evidence: `.cache/shuttle-work/massage-reminder-delivery-v3-qa/`.

The worker repairs durable jobs in bounded UUID pages. Before reserving a send,
it verifies current IAM authority and rechecks canonical booking, event module,
local revocation, due time and version. An existing accepted booking does not
require a freshly assigned pass to receive its reminder. Reservation persists a
unique attempt before network IO. Shutdown finalization uses the shared bounded
business-notification sender. A known rejection may retry after backoff; a lost
response, interrupted reserved attempt or failed outcome write becomes unknown
and is not automatically resent. Restart recovery requires the exclusive bot
instance lock. Safe retry and administrative retry rebase current settings under
settings SHARE before the reminder lock; the safe retry preserves its embargo.

Tests exercise send recovery, duplicate reservation, cancellation, settings
changes during an attempt, IAM waiting/outage, module disabling, local revocation,
known Telegram rejection, unknown delivery, exact authorized retry/dismiss,
immutable resolution receipts and failure to save a successful send outcome.
The last case observes one send, then unknown state on the queue retry and no
second send after a service restart. Existing payment, pair and order notification
paths are included because they share the sender and resolution machinery.

RU/EN reminders use frozen booking terms and absolute event-local start time,
including the IANA zone and DST offset. They make no claim that a configured
number of minutes remains or that payment was confirmed. Both independent
legacy reminders remain eligible for short-notice and late catch-up; there is no
implicit expiry/coalescing and no proactive Loom run. The optional Mini App
button opens the massage section, not a selected booking/event deep link.

Administrative `massage_reminders` and `massage_reminder` tools expose bounded,
deployment-scoped metadata and receipts, excluding send payloads. Existing
`resolve_notification` accepts kind `massage` with exact versioned proposals and
current global administrator authority. Retry warns about possible duplicates;
dismiss closes delivery without asserting receipt. Neither action changes a
booking or payment. Queue and worker metrics include bounded reminder labels.

Full configured lint passed after the delivery refactor. The last edits changed
only queue metric expectations and the API description; both affected packages
passed their native suites and configured lint. Fresh correctness, skeptical
quality and final source reviews found no actionable defects. The API description
nit was corrected. The first delivery gate passed its integration/text checks but
failed an old worker-series count (17 rather than 21). The second passed all 33
integration roots, text and worker metrics but exposed an old queue-series count
(12 rather than 15). Both failed runs were preserved and their exact resources
removed. The final gate passed; the failures are not replaced by its result.

## Owned installation and real-IAM API checks

The complete image `sha256:f65ec8c2e34a1e5a565d83773bb1ca35a57289ff25fe88b59a13081c95098854`
was built from 833 frozen release inputs with the unmodified Dockerfile. The
separate builder used a 2 GiB/no-swap limit and was removed with its cache.
The image remains available for this owned test stand.

The reviewed rollout installed schema47 / `mvp-51` on `shuttle-live-test` only.
A private PostgreSQL dump (2,693,732 bytes) had its TOC and checksum verified.
The bot exited normally in 0.782 seconds without OOM; this measures application
stop, not the full maintenance window. Historical rows, settings and exact
bot configuration were preserved. Migration added the empty reminders table and
nullable resolution target; it created no historical intents. The exact migrator
and private environment file were removed. The same Telegram poller resumed and
completed a fresh successful cycle, while all eight companion fingerprints stayed
unchanged. Evidence: `.cache/shuttle-live-test/massage-reminders-main-v1/`.

Independent rollout reviews identified and fixed three interruption windows:
created-but-unstarted migrator, cleanup after verified migration, and interrupted
setup before a migrator exists. A fresh final review found no actionable findings.
These recovery entrypoints were reviewed; this rollout did not inject those
controller interruptions and does not claim runtime fault acceptance for them.

Real local Zitadel tokens passed installed admin discovery and empty-list reads;
the owner received403 on list and detail, and an administrator received404 for an
unknown reminder. The table stayed empty. This is real-IAM authorization/API
acceptance, not real Telegram reminder delivery or a Loom tool-call acceptance.
Evidence: `.cache/shuttle-live-test/massage-reminders-real-auth-v1/`.

## Real Telegram delivery and populated Loom diagnostics

A new synthetic booking was created through the owner-authorized prepare/apply
API for the current account in `@complynx_test_bot`. It created exactly two new
pending reminder intents; historical bookings were not backfilled. An audited,
temporary 14-day long lead made only the long reminder due. Real Telegram accepted
message4549 for that account. The stored payload and receipt match its booking,
price and event zone. Current Telegram Web DOM also showed the reminder heading,
`2026-10-25T01:00:00+02:00 (Europe/Berlin)`, `45.00 BYN`, the booking reference and
an Open massage bookings button. This check did not capture a screenshot or open
that button. It does not claim full visual or navigation acceptance.

The original settings document was restored through a second audited change,
and the synthetic booking was cancelled through the owner API. The delivered
long reminder remains history; the short reminder is blocked. No future reminder
is left for this fixture, and the Loom run count did not change during delivery.
Evidence: `.cache/shuttle-live-test/massage-reminder-real-delivery-v1/`.

After delivery, real IAM authorized paginated list/detail reads for the two
retained reminders, with send payloads excluded. The ordinary owner remained
denied. A real Loom run (`d2c2522b-9208-49f6-96ef-dec3394fc62e`) invoked both
`zinusya_administration.massage_reminders` and `massage_reminder` through its
script broker, read the expected IDs and completed. A manual test provider drove
the script and final answer; this is not unattended model acceptance. Reminder
records stayed unchanged by diagnostics. The preceding empty-list run is also
preserved. Evidence: `.cache/shuttle-live-test/massage-reminders-real-admin-v2/`
and `massage-reminders-real-admin-v1/`.

## Remaining acceptance and policy

Real-service acceptance above covers one new synthetic booking and manual-provider
diagnostics. Delivery fault paths use mock peers in the component gate. Unattended
model behavior, full visual/navigation QA and production acceptance remain open.

Existing pre-feature bookings are not automatically backfilled; the scope
question is recorded locally for Daniel. Queue repair restores jobs only for
existing intents. Specialist identity bindings, staff reminders and rescheduling
remain later work. Migration down refuses any reminder rows so that delivery
history cannot be silently discarded. Do not install a partial feature.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
