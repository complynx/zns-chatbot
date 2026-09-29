# Massage domain migration slice

The Go package now includes transactional PostgreSQL persistence and authenticated
HTTP routes. This is not stage-3 acceptance: bot callbacks, Telegram/Web App
rendering, import execution, and actual notification delivery remain separate
integration work. See [massage-service.md](massage-service.md).

## Source rules

All references below are to `zns-chatbot/plugins/massage.py`.

- Lines 21-35: 20-minute slots, five-minute service buffer, two-hour party
  tolerance, five-minute specialist flyover, 15-minute client advance deadline.
  Python naive datetimes represent Europe/Minsk wall time. Go receives instants;
  the importer must attach Europe/Minsk before conversion, never interpret them as UTC.
- Lines 37-68: BYN prices 43/57/90/110/125/150; RUB prices
  1200/1600/2500/3000/3500/4000 for lengths 1..6. Service duration is 20*N-5
  minutes. Regular booking buttons expose lengths 1/2/3/5; specialist instant
  booking exposes 1..6 (lines 1043-1061).
- Lines 133-147: specialist selection minimum defaults to 1, maximum to 1000.
  The misleading `table_not_required` value is returned directly by
  `table_required`: true consumes a shared table; absent/false does not.
  Go names that input `LegacyTableFlag` to avoid silently reversing imported data.
- Lines 171-181: admit work spans only when span start is within party start minus
  two hours through party end, inclusive. Round first slot upward and end downward;
  negative slots and span ends after party end are valid. Union overlapping spans.
- Lines 223-238, 1206-1258: occupancy blocks the entire N-slot reservation,
  including the five-minute service buffer. Availability requires consecutive
  slots for the same specialist. One table is shared among flagged specialists.
  More tables than or equal to flagged specialists imposes no shared constraint;
  any other capacity is explicitly unimplemented in Python and returns ErrTables.
  Availability expires strictly after party end plus two hours.
- Lines 1181-1204: current party uses strict two-hour boundaries and configuration
  order. Slot indices use mathematical floor, including before party start.
- Lines 986-1014: only a specialist can instant-book; select the current slot,
  advancing one slot when its start is strictly more than five minutes ago.
- Lines 706-737, 812-838: clients require 15 minutes advance; specialists may
  reserve within five minutes after slot start. Equality passes. Client daily
  limit is configured (default 3 in `zns-chatbot/config.py:132`), waived for
  specialists. Selection excludes client occupied slots and the preceding N
  slots for an N-slot request, including the immediately adjacent reservation.

## Deliberate safety divergence

Python finalization at line 731 uses `slot not in available or specialist_id not
in available[slot] and slot not in my_occupied_extended`. Operator precedence
therefore permits an unavailable selected specialist when a client conflict
exists, and does not reject conflicts when that specialist is available. The Go
Eligibility function enforces both specialist availability and the client conflict
restriction displayed by the selection UI. Tests cover these previously unsafe
cases. This is a documented correction, not exact parity with that defect.

Go Available validates quoted lengths 1..6. Python's lower-level function accepts
other lengths, but normal active booking buttons do not expose them.

## Integration contract and remaining work

Load a consistent snapshot of active, finalized reservations scoped by event
(pass_key) and party. Exclude records with a deleted field; do not mix draft
records into occupancy. ClientBookings must additionally be scoped by client.
Load specialist work spans and table flags from that same snapshot. A Party is
one concrete event party, not merely the legacy day-of-month index (which can
collide across months).

Apply regular/instant allowed lengths, specialist min/max duration, authenticated
actor authorization and event membership in the application layer. Obtain
Available for the requested length, then run Eligibility. Neither function
performs authorization, locks or writes. In a transaction, serialize relevant
party/table/specialist/client reservations and re-read before deciding. Add real
PostgreSQL concurrency and retry tests before enabling writes. Validate durations,
identities and timestamp ranges when decoding persisted/configured input.

Implemented since the pure slice: migration 027, booking/cancel/instant commands,
owner-bound idempotency and version checks, specialist/admin timetable access,
notification preferences and a durable reminder outbox. Real PostgreSQL tests
exercise competing shared-table bookings, replay, cancellation and privacy.

Remaining parity: import mapping and execution; start/selection/cancel
callbacks and stale callback ownership; duration filters and specialist choices;
instant menu's nearest-booking/current-booking visibility; regular party/menu
selection; client/specialist views; notification preferences, reminders and
idempotent delivery; timetable/Web App; localized RU/EN catalogs; manual and
agent flows in a Telegram-like stand; both independent QA gates. The pure domain
unit fixtures alone do not accept any of those capabilities.
