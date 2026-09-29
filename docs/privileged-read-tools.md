# Privileged read tools

This slice adds six read-only Sobek descriptors to the existing live discovery registry. It does not complete the all-API roadmap and introduces no mutation, SQL tool, or new database migration.

| Tool | Arguments | Visible to |
| --- | --- | --- |
| `privileges.events` | optional `cursor` | Actor with any current pass payment-admin or massage practitioner role |
| `passes.payments.queue` | required `event`, optional `cursor` | Current payment admin in at least one event; actual read requires role in requested event |
| `passes.payments.history` | required `event`, optional `cursor` | Same payment-admin rule |
| `massage.practitioner.schedule` | required `event`, optional `cursor` | Current practitioner in at least one event; actual read requires requested event role |
| `massage.practitioner.preferences` | required `event` | Same practitioner rule |
| `massage.practitioner.bookings` | required `event`, optional `party`,`cursor` | Same practitioner rule |

List, help and worker bindings omit inaccessible tool names. Each discovery and call rebuilds current role evidence. Every data read independently enforces the requested event's current role; cursors never grant access. Global pass administrators and food administrators do not thereby gain these roles. Hidden payment administrators have payment-read access. A role remains valid when ordinary booking permission is disabled.

`privileges.events` returns only the actor's event IDs with current `payment_reads` and `practitioner_reads` flags. It includes historical pass events and massage-only events. No tool assumes the current order event; privileged data reads require an explicit event. Pagination binds actor, operation and filters and returns `items`, `next_cursor`, `more`.

Payment queue returns only pending attached payment reviews. History returns immutable attempt/participant snapshots, including replaced and rejected attempts. Shared attempts produce one row per participant; attempts without a participant remain visible with an empty participant reference. Historical rows contain no invented current booking version. Imported per-participant receiver/reviewer metadata is joined against the participant's historical assignment, not the current booking. An authenticated later reviewer supersedes imported reviewer metadata. Unknown actors stay unknown. No proof file, filename, legal name or passport is downloaded or exposed by history.

Practitioner schedule returns only the authenticated actor's work spans. Preferences are their two notification booleans. Assigned bookings include only active reservations whose specialist is that actor, optionally in the selected party. They do not expose colleagues' work, preferences, clients, or the full event timetable. Role locks keep membership valid through history and practitioner page reads; preference and queue reads use their existing current-role checks.

Core pages query one lookahead record and return at most20 items within24KiB JSON. History boundaries are receipt timestamp/attempt/participant; schedule and bookings use start timestamp/ID; event scopes use event ID. These are live keyset reads, not frozen cross-page snapshots. Queue reuses25-row Core pages and bounded20-item script subpages, detecting changes within a partially consumed source page as stale. Cursors are at most2048 bytes; event and party arguments are at most200 bytes. Practitioner cursors bind the exact operation, actor, event and party through a fixed-size digest of their canonical tuple, so maximum-length escaped IDs still produce usable continuations. Oversized atomic results fail explicitly; records are never silently truncated. Existing worker call/time/traffic limits and32KiB per-read/4KiB final model-result limits remain unchanged.

Successful intermediate read payloads stay in owner-private receipts and are omitted from subsequent model call summaries, with an explicit omission marker. The script chooses the bounded evidence it returns. Diagnostics contain only fixed operation names, outcomes, durations, byte counts and item/empty metadata; no event IDs, cursors, participants, arguments or content. Revocation prevents fresh reads; it cannot retract information already returned while authorized.

Acceptance requires ordinary/role-mixed actors, hidden payment admins, historical scopes, own-only practitioner records, grant/revoke between discovery and call/continuation, shared historical attempts, bounded complete traversal, malformed cursors, metadata canaries and bilingual Telegram-like interaction. Implementation tests do not replace independent Code QA and Functional Senior QA.
