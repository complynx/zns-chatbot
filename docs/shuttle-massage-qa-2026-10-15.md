# Massage schedule foundation QA — 2026-10-15

The versioned schedule/candidate foundation passed source checks and acceptance on
the owned `shuttle-live-test` stand. This is not booking or production acceptance.
See [the API contract](shuttle-massage-schedules.md) and
[legacy study and design](shuttle-massage-foundation-plan.md).

## Source verification

Model behavior covers distinct DST instants, occupancy versus treatment, work-span
containment, adjacent spans, grid rounding, pagination, configured zero tables,
required explicit zero/false fields and invalid references/bounds. Review fixed
oversized raw JSON ingestion and UTC/local year boundaries. Final independent
model review found no actionable findings.

Database and HTTP tests cover preparation without publication, original receipts,
changed retries, expired/stale/wrong proposals, failed authorization without writes,
assigned unpaid access, disabled modules, owner/admin separation, current binding,
revocation, current event bounds and stale candidate versions. Manifest validation
accepts a valid candidate request and rejects a page above its advertised limit.
Three independent DB reviews and repeated fresh API reviews found no actionable
findings. Lint initially found duplicate HTTP boilerplate and repeated literals;
small shared POST/operation validation helpers and schema constants resolved them.
The last fresh review included those changes.

All 173 enumerated integration tests passed in twelve fresh race-test processes,
along with every explicit `cmd`, `internal` and `migrations` package. The exact
enumeration/coverage plan and fourteen terminal zero-exit, non-OOM container states
are retained locally. Full strict lint reported zero issues. All twelve existing
MiniApp Node behavior tests also passed; those are not browser/WebView acceptance.

Isolated PostgreSQL DDL checks verified empty rollback and refusal of a populated
rollback with its schedule row preserved. This does not prove restoration of a
production backup. The isolated schema, completed QA containers and disposable QA
PostgreSQL container/anonymous volume were removed.

## Owned native acceptance

The runtime image is
`sha256:cff188a9edf98ba35603235a2f35bab9d5d1b8a9aa7203b9a631bcd79d14f282`,
with schema 25 and manifest `mvp-25`. A 2,163,794-byte custom-format PostgreSQL dump
was saved before migration, checked for `PGDMP` and listed with `pg_restore`.
Its SHA-256 is
`0aecdc50a306e0255c34f80b433dadecaadf18cc084dfba9164b20f900469fd0`.
Migration and rollout preserved prior application counts, confirmed/pending money
and event-context hashes before the explicit new synthetic fixture was created.

Real Zitadel issued administrator and owner tokens. Real Loom run
`548f1907-bc2f-4b19-8782-168f664f1f37` discovered the administrative write, obtained
an exact proposal, waited for approval and applied configuration version 1.
An ordinary owner could not prepare the same write. Preparation had not published
the schedule. Synthetic prices and public specialist identifiers were used.

Real owner run `d889f825-2fa0-4e95-9c45-a9e2ad0b9f01` invoked
`modules.zinusya_massage.schedule` and `candidate_starts` through Loom's script
broker. The assigned unpaid owner received twelve starts. The two repeated
Berlin times were `2026-10-25T02:00:00+02:00` and
`2026-10-25T02:00:00+01:00`; treatment/occupancy were 900/1,200 seconds.
Responses explicitly excluded reservation support. Bot restart preserved the
candidate set and the original administrative status receipt.

Additional native checks denied an owner's administrative read and an
administrator's owner read without a pass. Disabling the module denied owner
reads; re-enabling it required a fresh event version for candidates. The original
successful operation still returned its original receipt after a later event
version and deliberate expiration of that operation's proposal. Payment evidence,
confirmed money and user/run event-context hashes remained unchanged.

The first boundary-check helper incorrectly parsed `psql UPDATE RETURNING` output
as a bare UUID, ignoring the trailing `UPDATE 1` command tag. This caused a local
helper `ValueError` after module disable/re-enable and stale-version denial had
already succeeded. The saved checkpoint and first failure log were preserved.
The remaining checks resumed without repeating those edits, using a scalar CTE
query. This was not a bot or Loom defect; no new upstream issue was warranted.

## Limits

Loom used the owned manual Codex mailbox provider. These runs prove real broker,
authorization and approval plumbing, not unattended model reasoning. No new
Telegram message or WebView action was used in this slice. Existing Telegram
ingress/delivery evidence is separate; the CA/zitadeltg/WebView gate remains
deferred. No production state, Loom developer checkout or developer stand changed.
No bookings, customer timetable, staff authority, cancellation/refund, financial
collection, reminders or service availability guarantees are implemented.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
