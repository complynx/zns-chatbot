# Massage schedules and candidate starts

Zinusya stores massage configuration per event. The current foundation exposes
configured work windows and schedule-derived starts. It does not reserve time,
evaluate booking eligibility or promise that a specialist/table is currently free.

## Configuration

Migration 25 adds `shuttle_massage_schedules`,
`shuttle_massage_schedule_preparations` and `shuttle_massage_schedule_operations`.
An absent schedule is different from an empty available-slots result. Configuration
can be prepared while the event module is disabled, but participant reads cannot.
Migration rollback refuses retained configuration, proposals or receipts; export
them before any separately authorized destructive rollback.

The document has one currency/scale, `block_seconds`, `buffer_seconds`, services,
windows and specialists. IDs are stable public identifiers, not Telegram IDs or
IAM roles. Names require `en` and `ru` and remain untrusted display data. A service
has a positive block count and integer `price_minor`; these are configured prices,
not imported legacy defaults or confirmed money. No currency conversion occurs.

Occupancy is `service.blocks * block_seconds`. Treatment is occupancy minus
`buffer_seconds` once. For a 1,200-second block and 300-second buffer, one block
means a 15-minute treatment occupying 20 minutes. The buffer is not added again.
Each window defines its absolute start/end and configured table count. Every work
span references one window and fits entirely inside it. Spans for a specialist
must not overlap within the same window. Adjacent spans remain separate.

Windows must fit the event's current absolute bounds. There is no implicit legacy
two-hour grace period: configure legitimate early/late hours inside the event.
Stored instants normalize to UTC; participant responses show the event IANA zone
and per-instant offsets. An incompatible later event-bound edit causes a conflict
until configuration is repaired; reads do not silently clip the schedule.

Bounds: 48 KiB raw/normalized document, 16 services, 32 windows, 32 specialists,
64 spans per specialist and 512 spans overall. Windows are at most 48 hours.
Blocks are 60–3,600 seconds, buffer is nonnegative and shorter than one block,
services have 1–6 blocks, and windows have 0–64 configured tables. Scale is 0–4;
prices are nonnegative minor units up to 9,000,000,000,000,000. Labels allow up to
128 Unicode characters, with no control characters. Explicit `false`/zero is
required for `requires_table`, `tables`, `price_minor`, scale and buffer.

## Participant API

Static module `zinusya_massage` advertises two read methods:

- `schedule`: `POST /internal/agent/massage/schedule`, arguments `{event_id}`.
- `candidate_starts`: `POST /internal/agent/massage/candidate_starts`, arguments
  `{event_id, expected_event_version, expected_version, query}`.

Both use the common agent envelope with protocol version, operation ID and
arguments. Read operations create no business mutation or durable receipt. Resolve
an unclear event with `zinusya_context` for module `massage`; the final API call
requires an explicit event ID and independently checks entitlement.

Current identity binding, deployment, module enablement and the caller's assigned
pass are checked in one PostgreSQL snapshot. Assigned unpaid/overdue passes qualify.
Waitlist status or an administrative role alone does not. Disabled/foreign/missing
configuration or entitlement returns 404; revoked credentials are denied. No
specialist private identity, customer timetable or other participant data is exposed.

`query` contains `window_id`, `specialist_id`, `service_id`, absolute `from` and
`until`, optional exclusive `after_start`, and optional `limit` (default 20, max
100). The fixed `[from, until)` search range spans at most 24 hours. Supply the
event/config versions returned by `schedule`; stale versions return 409.

Starts follow an elapsed-time grid anchored to the window start, cannot precede
the observation instant and must fit full occupancy inside one work span/window.
A table-requiring specialist has no candidates when configured tables are zero;
non-table work is unaffected. A positive table count does not represent remaining
capacity. A candidate's start lies inside the search range; its occupied end can
extend beyond `until` while still fitting the work span/window.

Responses separate treatment/occupancy endpoints and durations and explicitly
return `schedule_only=true`, `reservation_supported=false`. DST-repeated local
times retain different offsets. Pagination returns `has_more` and `next_after`;
reuse the same fixed range/versions with `query.after_start=next_after`. A later
request has a later observation instant, so starts that have become past disappear.

## Administrative API

Permission-scoped discovery exposes `massage_schedule` and
`set_massage_schedule` only to the deployment's current global administrator.
The read path is `POST /internal/agent/administration/massage_schedule` with
`{event_id}`. It does not require an assigned pass and returns the stored UTC
configuration and its version.

`set_massage_schedule` uses
`/internal/agent/administration/set_massage_schedule` and its `/prepare` and
`/status` paths. Arguments contain `event_id`, `expected_event_version`,
`expected_version` and the complete document. Version zero creates missing
configuration. Preparation requires a nonzero operation UUID and records a
five-minute exact proposal without publishing it.

Apply requires matching operation/idempotency UUIDs and confirmation
`{mode: "exact_parameters", preconditions: {proposal_id}}`. The server rechecks
current administrator/account authorization, event bounds and event/config
versions. Schedule and original receipt commit atomically. Changed retries,
expired first apply and stale approvals reject without publishing configuration.
Successful same-operation replay returns the original result after later edits or
proposal expiry. After uncertainty, reconcile the same operation with `/status`;
missing status is unknown and does not authorize a replacement operation.

HTTP bodies are capped at 64 KiB for configuration writes and candidate requests;
simple reads/status use the common smaller internal limit. There are no new
libraries, external processes or outbound specialist messages in this foundation.

## Acceptance boundary

Model, database and HTTP behavioral checks and fresh independent reviews passed.
All 173 enumerated integration race tests, other explicit Go packages, full lint
and owned real IAM/Loom approval/read/restart checks passed. See
[native QA and its limits](shuttle-massage-qa-2026-10-15.md). Booking, rescheduling, cancellation, staff self-service,
payment collection, notifications and timetable UI remain later work. Open booking
lifecycle choices are recorded in the ignored operator decisions file.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
