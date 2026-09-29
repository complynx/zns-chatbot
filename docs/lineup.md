# DJ lineup domain

`platform/internal/lineup` ports the timetable queries from
`zns-chatbot/plugins/assistant.py`. The runtime connection uses selected skills;
Telegram-like functional acceptance remains a separate gate.

Integration: call `lineup.Parse(reader, eventYear, eventLocation)` on an
operator-configured CSV. Both year and timezone are required. Retain the returned
immutable schedule, then call `Current(now)`, `Day(now)`, and `Full()` when building
assistant context. All query results are copies. There is no network access,
filesystem access, process clock dependency, database, or external dependency.

The CSV has quoted `weekday, DD.MM` date headers, a second row of room names,
and subsequent rows with `HH:MM` then DJ names. Columns are rooms/dates. Cells and
room names are trimmed. Empty cells, short data rows, and invalid time rows are
skipped as in Python. Empty input is an empty schedule. Invalid headers, missing
rooms, malformed CSV, invalid UTF-8, and input over 1 MiB return errors. The
timetable is limited to 128 header columns and 10,000 rows including headers to
bound parsing work for ragged CSV. Dates use
the explicit event year; 31 December early-morning rows cross into the next year.
Headers spanning December and January still need an explicit future extension;
the source format carries no per-column year.

Rows before 07:00 start on the calendar day after their column date. Current sets
last one elapsed hour with an inclusive start and exclusive end. Gaps remain
gaps; there is no inference that the last DJ keeps playing. Full groups use the
column's event date and sort by date then room. Equal start times retain CSV
column/row order.

The daily query deliberately preserves a source quirk: before 07:00 it selects
the previous date, then includes the entire selected calendar date and the next
morning before 07:00. Consequently it may include the previous event night's
early-morning rows. Full grouping assigns those rows to their original event day.
Changing this overlap requires a separate behavior decision.

Timezone conversion applies to queries. Nonexistent or ambiguous local set starts
at DST changes are rejected because the CSV has no offset/fold field. No host
timezone or current year is inferred.

## Runtime integration

Configure all three settings together: `LINEUP_CSV`, `LINEUP_EVENT_YEAR`, and
`LINEUP_TIMEZONE` (for example `Europe/Minsk`). Canonical configuration names are
`ZNS_LINEUP__CSV`, `ZNS_LINEUP__EVENT_YEAR`, `ZNS_LINEUP__TIMEZONE`, or YAML
`lineup: {csv: ..., event_year: 2026, timezone: Europe/Minsk}`. Host-local timezone
`Local` is refused. The single configured source describes one event year's
timetable; there is no cross-event lookup or inferred current-year replacement.

The bot loads the local file once on startup. Restart after replacing the CSV to
reload it. Invalid configuration fails validation. An unreadable or malformed CSV
logs a generic reason and leaves lineup unavailable while other bot functions
continue. File paths and raw CSV errors never enter model context. Unconfigured
lineup also means unavailable, not an empty timetable.

Each request snapshots one host instant and converts it to event time. A bounded
internal transport snapshot supports both direct providers and the private Remote
model route. The selection prompt and unrelated planning prompts receive no
lineup data. `lineup_current`, `lineup_day`, and `lineup_full` have separate use-when
catalog entries and skill bodies. Only selected scopes reach planning context.
Scope results are capped at 8 KiB each; `omitted=true` explicitly marks incomplete
coverage and carries `next_cursor`. The read-only `lineup_action` queries the full
host source with scope, optional event date, exact case-insensitive room, literal
case-insensitive DJ substring, and a match cursor. The next cursor with unchanged
filters reaches every matching page; filters operate on original labels. Four
reads are available per request. At exhaustion the assistant must disclose partial
coverage and the continuation cursor; a later request can resume at that cursor.
Labels over 256 runes are shortened with `truncated=true` so an oversized row
cannot block pagination to later entries. Source data remains immutable until
restart. Cursors are authenticated opaque tokens bound to this startup source,
filters and original query instant; fabricated, modified, cross-query or pre-reload
tokens are rejected. A continuation's `as_of` preserves that instant, including
the original event-day window; it is not a fresh claim about current playback.
No source file path is transported.

Current empty results use `no_current_party`, day empty results use
`no_party_today`, and a verified empty full timetable uses `no_schedule`.
Load/configuration absence uses `unavailable`; it must not become a no-party
claim. Full and daily lists do not establish current playback. Skills instruct
responses in the current user's language and treat DJ/room text as untrusted data.
The read proposal has no identity, filename, network access, mutation or extra
permission. `no_matches` applies only to query filters/page, never to party status.
