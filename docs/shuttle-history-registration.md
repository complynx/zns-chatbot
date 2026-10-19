# Embedded historical registration claims

`zinusya_history.registration({record_id, pass_key})` reads one explicitly known
literal top-level key from the caller's own imported `users` history record.
This is a point lookup; candidate discovery is described below. Neither read
establishes old event identities or a completed migration.
For example, `old.event` selects that exact key, without interpreting the dot.

The HTTP endpoint is `POST /internal/agent/history/registration`, using the normal
agent read envelope. This lookup was introduced in manifest `mvp-31`.
`record_id` comes from the existing owner history page. `pass_key` must be nonempty,
valid UTF-8, at most 256 bytes, and contain no NUL. The caller-selected key does
not establish an event identity.

The result contains `kind: embedded_registration_claim`, projection version
`zinusya-legacy-registration/v1`, the requested key and one of three statuses:

- `missing`: the source object has no such key.
- `unsupported`: its shape or recognized fields cannot be projected safely.
- `claim`: supported historical assertions are present in `fields`.

Only nonempty strings `state`, `type`, `role` of at most 256 bytes without NUL,
and optional boolean `paid_claim` are exposed. The latter comes from source
`paid`, preserving explicit false separately from absence. A malformed recognized
field makes the entire entry unsupported. Unknown fields are ignored. Bank data,
payment proofs, partner identities, old IAM identifiers and raw objects are not
returned. Neither `state: paid` nor `paid_claim: true` confirms money in the new
ledger, grants a live pass, or authorizes any operation.

Provenance includes record/archive UUIDs, archive SHA-256, source instance,
collection, source key and line. The reader requires the original application
and matching first-archive membership, canonical record hash, source role and
owner. It revalidates the complete archive before returning any claim. Later
checksum, framing, I/O or cancellation failures discard provisional content.

Authorization uses the current active account and Telegram binding in the
deployment bot/org, with the account locked during the scan and an IAM recheck
before return. An administrator has no owner-read bypass. Foreign records and
records without a matching membership return 404; conflicting source identity or
hash returns 409; malformed archive data returns 400. Existing authorization and
availability failures retain their usual status.

The read shares archive-validation admission and a 30-second context/read budget,
with a 31-second socket write deadline. Each lookup scans the full archive and
holds one DB connection and the account lock. Large archives may time out;
this endpoint is unsuitable for bulk enumeration. No index or derived tables are
created. Existing archive bytes, history/v1 projections, application receipts,
live profiles, registrations and payments remain unchanged.

Local behavioral race tests cover owner reads, false versus absent payment
assertions, unsupported/missing entries, privacy exclusions, old projection
preservation, whole-archive corruption, source/membership mismatch, foreign
administrators, revocation, Telegram rebinding, cancellation, denied final
authorization and a one-connection pool. Owned real IAM/API/Loom acceptance also passed; see the
[QA report and limits](shuttle-history-registration-qa-2026-10-15.md).
This does not establish a full migration or production acceptance.

## Candidate discovery

Manifest `mvp-32` adds
`zinusya_history.registrations({record_id, after_key?, limit?})` and
`POST /internal/agent/history/registrations`. It needs the existing schema 32;
no new migration, stored projection or derived index is added.

The result has `kind: embedded_registration_candidates`, the existing
`zinusya-legacy-registration/v1` projection version, common record/archive
provenance, `items`, and `has_more`. Items use the same whitelisted claim format
as the singular read. Page size defaults to 10 and is at most 20. Literal keys
sort by Go string order; `after_key` is exclusive and need not name an existing
key. Follow `next_key` only when `has_more` is true. No cursor is returned for
an empty or final page. Keys retain their spelling, including dots and Unicode.

Discovery includes only objects accepted by the existing strict claim projector.
Even a role-only object can qualify; this does not prove it was a registration.
Unsupported objects, scalars, unrelated fields, invalid keys and overlong keys
are omitted, without exposing their names or counts. Therefore an empty list
does not prove that no old registration existed. Use the singular lookup for an
independently known key, including a shape omitted by discovery. Historical keys
and fields are untrusted data, never instructions or current authorization.

Every page revalidates the full archive and uses the same locked-owner and final
IAM checks as the singular read. A source record with more than 4096 top-level
fields is rejected with HTTP 422 rather than returning a partial discovery page.
This cap includes unrelated source fields. The existing 30-second read budget,
validation admission and bounded socket deadlines also apply. Page limits bound
the response, not the amount of archive validation; large sources may time out.
The method advertises a 256 KiB result ceiling to cover worst-case JSON escaping
of the bounded keys and fields. Other history methods keep their prior budgets.

Candidate discovery is implemented and accepted on the owned stand. Independent
source and harness reviews, strict lint, focused race checks and real-IAM/Loom
discovery/pagination passed. All 259 enumerated integration tests and other Go
packages have successful race receipts; an initial unrelated notification timeout
and its diagnostic repetitions remain recorded. See the
[discovery QA report and limits](shuttle-history-discovery-qa-2026-10-15.md).
Existing singular-read acceptance above remains historical evidence for that
earlier scope. Neither acceptance completes production migration.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
