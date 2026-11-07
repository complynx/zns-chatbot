# Massage schedule foundation

Architecture and senior Go consultations agreed on event-bound configuration and
owner schedule/candidate reads as the next slice. Booking writes remain separate:
legacy code contains conflicting rules, so it is not a reliable policy source.
This plan does not claim implemented or accepted reservations.

## Legacy evidence

The active implementation is `zns-chatbot/plugins/massage.py`, not the unrelated
root `massage.template.yaml`. Its relevant behavior is:

- Occupancy uses 20-minute blocks; treatment is `blocks*20−5` minutes. Public
  choices are 1/2/3/5 blocks; specialist instant reservations allow up to 6.
- BYN and RUB prices are separate historical tables, not an exchange formula.
  These Grodno values are not defaults for the new module.
- Specialists have working spans, duration limits and notification preferences.
  Whole consecutive blocks must fit the same specialist's working time.
- Normal clients have a 15-minute lead and a default limit of three per party.
  Specialist identity bypasses those checks and allows a five-minute late start.
- Cancellation soft-deletes and notifies; no explicit cutoff/refund/ledger policy
  exists. Notifications save their sent flag before network IO.

Do not copy legacy defects: `table_required` returns `table_not_required` unchanged;
limited shared tables beyond the one-table case are unimplemented; finalization's
Boolean grouping can admit overlaps; duration checks occur only in menu rendering;
by-ID reads/writes lack adequate owner scope; the timetable exposes all customers
to any specialist/food administrator. A process-local finalizer lock is not durable
multi-process concurrency control. Naive Minsk times, day-of-month window IDs and
frontend fixed UTC+03 are incompatible with event timezone and DST requirements.

Evidence is in `massage.py:20–67,145–179,294–340,540–654,706–745,986–1059,1104–1256`
and `static/massage_timetable.mjs:49–56,85,154–157`. The old two-hour tolerance is
inconsistent between window selection, span inclusion and current-party logic.

## Selected contract

Use a bounded versioned per-event massage document, stored in PostgreSQL. Admin
updates use existing exact prepare/apply/status, expected event/config versions,
five-minute proposals, normalized input hashes and original receipts. No generic
module framework or new runtime dependency is needed.

The document has one explicit bounded `block_seconds`/`buffer_seconds` pair,
currency/scale, services with block length and integer minor-unit price, stable
windows with absolute endpoints/shared-table counts, and specialists with public
IDs, en/ru labels, explicit `requires_table`, service references and working spans.
No Telegram/IAM specialist identity or customer data is included in this catalog.
Treatment is `blocks*block_seconds−buffer_seconds`; full occupancy is exactly
`blocks*block_seconds`, not treatment plus a second buffer.

Windows must lie inside the event aggregate. Operators explicitly include early
or late specialist hours in that aggregate; no hidden two-hour extension applies.
Working spans lie inside their window, with no overlapping duplicates per
specialist/window. Adjacent spans remain separate: full occupancy must fit one
span, never silently bridge a break. Later incompatible event edits make reads
return a configuration conflict instead of silently clipping time.

Owner reads reuse current binding, event/module enablement and assigned-pass
entitlement in the same snapshot. Assigned unpaid/overdue qualifies; waitlist or
administrator status alone does not. Use the existing event-context resolver for
module `massage`, not another nearest-event selector.

Candidates require one window, specialist and service, current event/config
versions, an absolute `[from,until)` range of at most 24 hours, optional exclusive
`after_start`, and a bounded page. Starts follow the elapsed-time grid anchored to
the explicit window start and cannot precede the observation instant. Full
occupancy must fit one work span/window. A table-requiring specialist with zero
configured tables has no candidates. These reads do not inspect reservations or
apply client/specialist booking exceptions. Responses explicitly state
`schedule_only=true`, `reservation_supported=false` and separate treatment from
occupancy. Event IANA timezone and per-instant offsets distinguish DST repetitions.

Non-goals: booking/rescheduling/cancellation, specialist self-service, money/FX,
notification delivery, recurring schedules, production import and timetable UI.
These remain later lifecycle work, not completed by configuration or candidates.

## Storage contract

The pure schedule model, candidate calculation and PostgreSQL storage layer are
implemented. Migration 25 adds separate versioned schedules, exact five-minute
proposals and durable original operation receipts. SQLc supplies their queries.
Successful replay preserves the original receipt after expiry and later edits.
Current binding, module enablement and assigned-pass entitlement share one read
snapshot; candidate calculation runs after that snapshot releases its connection.
Incompatible later event-bound edits return a conflict rather than clipping work.

Model review fixed oversized raw JSON ingestion and UTC/local year boundaries.
Isolated PostgreSQL DDL checks verified an
empty rollback and refusal of a populated rollback with the schedule retained.

No booking is implemented.

## Gates

Focused behavior checks cover normalization, required zero/false fields,
references/bounds, overnight/DST windows, disjoint spans, full-block fit, zero
tables, limits/cursors and stale versions.
DB/API acceptance adds unpaid access,
waitlist/admin/foreign/revoked/module denial, exact approval/replay/changed retry,
event edits and restart.
No production state is read or changed.
