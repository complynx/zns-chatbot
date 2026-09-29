# Massage persistence and API

Migration 027 adds event settings, ordered parties, event specialists, work spans,
finalized reservations, command replay records, and a durable notification outbox.
It does not replace generic `core.slots`; the old generic booking fixture is a
different workflow. No production or shared-stand data was migrated or seeded.

All dates are PostgreSQL instants. Legacy naive timestamps must be interpreted
in Europe/Minsk by an importer. Parties use unique IDs instead of the Python
day-of-month key. Preserve configuration order in `position`, the exact
`table_not_required` truth value in `legacy_table_flag`, localized specialist
about text, and minimum/maximum length values. Database bounds reject malformed
configuration: parties/work spans must be positive and no longer than two days,
tables and daily limits are bounded, and supported booking lengths are 1–6.
Import validation must report rejected legacy data; it must not silently clip it.

The existing authenticated API wrapper resolves actors. No command accepts an
owner supplied by the caller. There is no invented pass or generic `can_book`
requirement: Python massage booking does not use that generic fixture permission.

| Route | Purpose |
| --- | --- |
| `GET /v1/massage/parties?event=...` | Ordered event parties and daily limit |
| `GET /v1/massage/slots?event=...&party=...&length=2` | Eligible choices and provider/work metadata |
| `GET /v1/massage/bookings?event=...&party=...&view=mine` | Actor's reservations, including cancellations |
| Same route with `view=clients` | Specialist's active client list |
| Same route with `view=timetable` | Active event bookings, specialist/admin only |
| `GET /v1/massage/timetable?event=...` | Complete timetable metadata and authorized client identities |
| `POST /v1/massage/actions` | `book`, `instant`, or `cancel` command |
| `GET/PUT /v1/massage/preferences?event=...` | Specialist's own booking/next-notification preferences |

Slot selection exposes a dedicated public provider DTO: professional name,
icon, localized description, supported lengths, and the specialist contact ID.
That Telegram ID is retained for links to the listed professional, as requested;
it is not a client's Telegram ID. Python already exposes specialist IDs in its
timetable and specialist choices. Work schedules, table flags, and notification
preferences are excluded from this public DTO. Preference fields also never
serialize through the internal Provider type; the owner-bound preferences route
is their only API surface.

Commands contain a stable `key`, `action`, and `event`. Regular booking also
uses `party`, `specialist`, `slot`, and `length`. Instant booking uses `party`
and `length`; the server derives the current slot and forces the specialist and
client to the actor. Cancellation uses `booking` and its current `version`.
Identical actor/key replay returns the original result. Different content under
the same key returns `idempotency_conflict`. Replays do not repeat writes/notices.

Booking and cancellation lock the party row. Booking re-reads staff, work spans,
and active reservations under that lock, then checks table and specialist
occupancy, time bounds, client adjacency conflicts, and the per-party daily cap.
The cap is waived for specialists as in Python. Regular lengths are 1/2/3/5;
specialist instant lengths are 1–6. Specialist duration filters apply to regular
selection; Python's instant buttons do not apply those filters. Instant booking
uses the first current party in configuration order. Cancellations remain
available after start because Python has no cancellation cutoff.

Prices are snapshotted on the booking: BYN 43/57/90/110/125/150 and RUB
1200/1600/2500/3000/3500/4000. Actual service duration is 20*N−5 minutes;
occupancy includes all N twenty-minute slots. Shared-table configurations that
Python explicitly does not implement return `unsupported_table_configuration`.

Python's food administrators may see the timetable; they have no special
booking/cancellation override. This slice maps that existing role through
`core.order_admins`. Specialists can see all timetable entries and their own
client list. Only the reservation owner may cancel. The original Python callback
loaded arbitrary IDs without checking ownership; rejecting that forged-ID path
is a deliberate security correction, alongside the previously documented
conflict-condition correction.

## Notifications and next integration

Booking and cancellation append specialist notices in the same transaction,
respecting booking preferences. Instant self-reservations suppress the new-booking
notice. `QueueReminders(event)` is a trusted maintenance method with unique
per-booking/recipient/kind keys. It queues client long/short reminders and the
specialist five-minute reminder according to preferences. Default leads are
one hour and ten minutes. Python's strict upper threshold is retained. Reminder
catch-up starts at the earliest party start minus two hours, including already
started appointments inside that source window. A new-booking notice is eligible
through the booking start and expires strictly after it; this expiry does not
suppress the separate long, short or next-appointment reminders. Cancellation
notices remain durable. Candidate28 contains this parity correction; independent
acceptance is tracked in PROGRESS.md.

`PendingNotices(actor)` returns at most 100 unsent notices belonging to that
recipient. `AcknowledgeNotice(actor,id)` only acknowledges that recipient's
notice. The single bot poller should send first, then acknowledge, so failures
retry instead of marking delivery before sending as Python did. A crash between
send and acknowledgment can duplicate a message; no exactly-once delivery claim
is made. Canceled bookings suppress unsent booking/reminder notices.

Notices are not exposed through a public delivery API. The bot poller queues
reminders for parties within the existing two-hour event tolerance, then attempts
at most ten notices per poll. Failed sends stay pending. A stored Telegram message
ID makes retries edit the same notification; the crash gap before storing that ID
can still duplicate a send. Delivery also refreshes an opened recipient menu.

`/massage` opens a separate persisted Telegram card. Visitors choose a party,
duration, specialist and available start, then see their bookings and cancellation
buttons. Specialists also see clients, timetable, notification preferences and
instant self-reservation. Timetable permissions are rechecked by the API. Times
are labeled Minsk (UTC+3), and both English and Russian catalogs are supported.
Modern choice pages contain eight items; booking pages contain five. Imported
legacy draft slot pages retain the Python page size of24, so saved page numbers
keep their original meaning. Navigation is shown when an adjacent page exists.
Booking prices in
cards and notices use the booking snapshot. This is a Telegram timetable view;
the legacy browser timetable and legacy data importer remain separate work.

Migration 029 stores each owner's current view, revision, card and callback
records. Buttons bind owner, revision and explicit action; mutations reuse the
button token as their idempotency key. Each callback rechecks domain permissions,
versions, timing and availability. Successful renders prune obsolete buttons.
The card is separate from order cards so order reconciliation cannot replace it.
No arbitrary specialist assignment or administrator configuration UI is added.

The two complete typed locale catalog functions have a scoped funlen exception: exhaustive key checks remain enabled and these functions contain declarative translations without branching. Their existing duplicate-key-structure exception remains scoped to the same functions. Both complete functions are included in fresh Code QA.


Authorized staff client cards expose a contact button using tg://user?id. This preserves Python's client_user_link_html client list and the authorized timetable name/id contract (massage.py:602,1309; server.py:679). Client contact IDs are not in public availability responses.

Code QA's concurrent-overbooking concern is covered by the existing party lock:
booking calls `loadParty(..., true)`, which selects that party `FOR UPDATE` before
reading reservations and checking availability. Cancellation uses the same lock.
The PostgreSQL concurrent-table test verifies one winner for competing requests.
No additional lock or transaction behavior change was needed for that finding.

