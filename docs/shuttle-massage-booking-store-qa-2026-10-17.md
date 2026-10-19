# Owner massage booking backend: component checkpoint

Date: 2026-10-17. At this component acceptance: source schema 46 / manifest `mvp-45`; the owned runtime
remains schema 45 / manifest `mvp-44`. Migration 46 has not been applied there.
This checkpoint accepts the domain/store component only. Booking transport,
Mini App forms, agent approval flows and real Telegram booking are still open.

A later [atomic Mini App store and agent API checkpoint](shuttle-massage-agent-api-qa-2026-10-17.md)
advances source to `mvp-46`. It does not install the runtime or accept native forms.

## Implemented behavior

A five-minute proposal freezes event/schedule versions, selected service,
specialist, party window, event timezone, price, treatment and occupancy bounds.
Preparing a proposal does not hold capacity. New booking checks current binding,
enabled module, assigned pass, versions, database time, minimum notice, party
count, owner/specialist overlap and shared table capacity inside one transaction.
Assigned unpaid passes qualify. Public service lengths are 1, 2, 3 or 5 blocks;
minimum notice is 15 minutes. The default maximum is three bookings per party
window. Runtime setting `massage_max_bookings_per_window` permits 1–100;
omitted or zero retains the legacy default of three.

The operation lock precedes the shared event row lock and current owner check.
Configuration changes use the same event lock. Full half-open occupancy includes
the buffer once. Shared tables use peak simultaneous occupancy, including
cross-window owner/specialist conflicts. A selected party window that overlaps
another configured window cannot be booked in this first owner slice. Existing
schedule-only candidate reads remain explicitly separate from live availability.

The canonical consumed proposal returns its original booking across interfaces
and new operation IDs. Each operation retains its immutable receipt. Reopening
the store or cancelling a booking does not rewrite the original book result.
Cancellation retains history and releases capacity; it requires current owner
binding, but no new pass or enabled module. It creates no fee, refund or payment
confirmation. A current record read distinguishes later cancellation from the
original successful booking receipt.

Schedule changes validate future/ongoing commitments in bounded pages and check
aggregate table capacity. Event updates cannot exclude an active future/ongoing
booking from event bounds. Migration rollback refuses retained proposals,
bookings or receipts. Structured legacy payment import remains out of scope.

## Verification

Architecture and senior Go consultations preceded implementation. Independent
correctness and quality reviews of the final component diff found no actionable
findings. Configured lint passed with zero issues. SQLc regeneration was byte
identical to the generated files used by the tests.

The isolated Linux/Go 1.27.2/PostgreSQL 17 race gate passed all 72 selected test
roots: 11 massage, 33 DB, 13 API and 15 integration tests. Its 145 passing test
events include subtests; there were zero failures or skips. The 948 frozen source
inputs remained unchanged throughout the gate.

New behavior checks cover DST repeated local times, price/buffer snapshots,
public notice/grid/duration restrictions, peak capacity versus consecutive use,
exact adjacency, independent owner/specialist/table contention, cross-interface
proposal reuse, store reopen, original receipts after cancellation, slot release,
changed/expired/foreign proposals, assigned-pass denial, schedule duration guards,
historical cancellation after pass loss, runtime party limits, owner pagination
and refusal to erase populated history. Neighboring event/settings tests also
passed. A subsequent two-root Linux race gate also passed direct event-bound
shrink and aggregate table-reduction checks: both changes are rejected while
booked, then succeed after cancellation. That gate froze 949 inputs, had no
failures/skips/OOM, preserved all ten Compose services and removed its disposable
containers and volume. Its evidence is under the ignored
`.cache/shuttle-work/massage-booking-guards-v1-qa/` directory. At this checkpoint,
revocation during a wait and single-connection Mini App composition still needed
coverage. The later [atomic store/API gate](shuttle-massage-agent-api-qa-2026-10-17.md)
now accepts that coverage.

The disposable Go container had a 2 GiB memory limit and PostgreSQL 512 MiB,
with swap disabled. Neither was OOM-killed. Both containers and their private
volume were removed after acceptance. The ten existing Compose services retained
their IDs, images, start times and restart counts. The synthetic counted provider
was not changed. Evidence is retained locally under the ignored
`.cache/shuttle-work/massage-booking-store-v1-qa/` directory.

## Next acceptance boundary

The later atomic store/API checkpoint accepts shared session transactions and
exact agent prepare/apply/status. Remaining work is current live availability,
Mini App HTTP routes, native owner forms and durable action context. Then verify native,
agent and mixed paths on the owned stand. Specialist identity/exceptions,
notifications, timetable, rescheduling and broader lifecycle parity remain open.
Browser-origin permission is pending independently; do not treat this component
gate as real Telegram or unattended Loom acceptance.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
