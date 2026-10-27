# Offline legacy snapshot inspection

`shuttle-import` validates an operator-prepared local export. It does not connect
to MongoDB, PostgreSQL, Zitadel, Loom or Telegram. It does not import users, create
tasks/passes, credit payments, fetch images, or change source files. This is the
first import preparation slice, not a completed migration or cutover tool.
An optional `-output` now creates a [verified private archive](shuttle-import-bundle.md);
the inspection-only mode described here remains the default.

## Source contract

Keep a private, immutable snapshot directory containing a version 1 manifest and
UTF-8 MongoDB Extended JSON files, one JSON object per line.
Supply at most one
file for each role: `users`, `messages`, `passes`, `files`. `users` is required;
omitted optional roles appear as absent in the report.
Each collection has its
actual source collection name, a local relative file path and the expected
lowercase SHA-256 of the exact file bytes.
Keep the manifest with the snapshot.

Example structure, with synthetic identifiers and a placeholder checksum:

```json
{
  "version": 1,
  "source_instance": "legacy-main",
  "bot_id": 123456,
  "date_policy": "preserve_source",
  "collections": [
    {
      "role": "users",
      "name": "zns_bot_users",
      "path": "users.ndjson",
      "sha256": "0000000000000000000000000000000000000000000000000000000000000000"
    }
  ]
}
```

Replace the checksum with `sha256sum users.ndjson` output. Export a consistent,
explicitly selected bot scope. Every users/passes/files record needs matching
`bot_id` and a positive exact `user_id`; all message/pass/file owners must exist
in the users export. Both manifest `bot_id` and every `user_id` must be below
2^52. Assistant messages lack `bot_id` in the old application, so
their collection entry must additionally declare `single_bot_collection: true`.
This is the operator's source-scope assertion, not inferred ownership. If messages
do contain `bot_id`, it must match. Never bind a shared multi-bot history collection
without first establishing which records belong to the selected bot.

Record `_id` must be a nonempty string of at most 256 UTF-8 bytes or an Extended
JSON ObjectId. Plain JSON
integer IDs and `$numberLong`/`$numberInt` wrappers preserve integer precision.
Integral relaxed JSON doubles (such as `42.0`) and canonical `$numberDouble`
wrappers are also accepted for bot/owner IDs. Decimal values are checked exactly,
without float rounding; fractional, nonfinite and out-of-range IDs are rejected.
Double spellings are limited to 64 bytes and exponents from -400 to 400.
Timestamp validation retains the stricter integer-millisecond representation. Duplicate source IDs, changed content at the same
source ID, repeated user identities, foreign bot IDs and orphan owners fail
validation. Duplicate JSON properties and case aliases in manifest properties
also fail. Paths stay inside the manifest directory; final symlinks, nonregular
files and multiple role paths referring to the same file are rejected.

Messages need a MongoDB `$date` value in canonical integer-millisecond or relaxed
RFC3339 form with an explicit offset. Dates are checked for supported shape and
left unchanged. The old assistant used `datetime.now()` without a zone: a BSON
timestamp in an export does not prove that its recorded instant matches the
original human local time. No Moscow/Minsk/Yekaterinburg conversion is inferred.
Historical date correction remains a separate decision.

The inspector does not rewrite unknown fields. Embedded `user[pass_key]` history,
payment assertions, identity metadata and avatar references remain in the exact
input files. Its standalone pass count does not include every embedded legacy
registration. The old messages collection contains saved assistant exchanges,
including user messages that may have no reply; it is not a complete Telegram
transcript and does not prove response delivery. File metadata or
`completed_simple` does not prove that generated avatar bytes remain available.

## Operator use and result

Build the separate CLI on the operator's platform, then inspect a local snapshot:

```sh
go build -o /tmp/shuttle-import ./cmd/shuttle-import
/tmp/shuttle-import -manifest /private/snapshot/snapshot.json > /private/snapshot-report.json
```

The application Docker image contains the bot and avatar renderer; it does not
ship this operator CLI. No live database credentials are needed for inspection.
The report contains fixed-order role counts, bytes actually read, source checksums
and issue locations/codes. It excludes message/profile contents, owner identifiers
and Telegram file references. `source_instance` is nonsecret provenance metadata.
Keep the actual input files private: excluding content from the report does not
redact the source archive.

A failed scan can
report only a processed prefix and omit its full-file checksum.
Issue locations
are role plus 1-based line; line 0 means a collection-level issue.
Rerunning the
same snapshot gives the same report and does not modify the snapshot.

Fixed bounds: manifest 64 KiB, four files, each NDJSON line below 8 MiB, aggregate
source read budget 256 MiB plus a one-byte overrun probe, at most 100000 validated
records, JSON nesting 64 and 1000 reported issues. Rejected overflow records can
also appear in scanned counts. Failed reads also consume the byte budget.
The report marks issue truncation. Larger archives need a separately reviewed
batching policy, not silently increased limits.

## Acceptance and next slice

The architect and Go senior agreed this bounded step before implementation.
Behavioral tests cover precise Extended JSON IDs/dates, repeatability, unchanged
source bytes, omitted-role coverage, privacy, bad checksums, foreign/orphan owners,
source/identity conflicts, malformed/deep/oversized JSON, manifest field aliases
and hard-linked source aliases.

This tool
adds no dependencies and changes no runtime schema or API.
Its separate native
Linux acceptance uses synthetic four-role exports, no network, a nonroot user,
read-only mounts/filesystem and a 128 MiB limit.
It checks identical reports,
exit 1 for an orphan, exit 2 for unbound messages, private-content exclusion and
unchanged input digests.

Verified archive creation is now accepted separately; see
[archive behavior and native QA](shuttle-import-bundle.md). Receiving verification
has its own [CLI and administrative API contract](shuttle-import-receiving.md).
Durable exact-approved storage has a separate
[contract and acceptance](shuttle-legacy-archive-storage.md). Next: identity-conflict
reconciliation, controlled user/history import and owner-scoped agent history reads. Archive
history must stay separate from live Telegram metadata and operational queues.
Old payment assertions must remain historical claims, never new ledger credit.
Avatar-byte export and preservation need their own proof. No production snapshot
was read and no applied migration is claimed by this step.
