# Durable legacy archive storage

This operation preserves a verified legacy archive in PostgreSQL. It does not
apply history, create local or IAM users, change current profiles, create passes,
enqueue Telegram replies or credit historical payments. Archive SHA deduplication
does not establish compatibility of individual records across different archives.
Source-key conflict checks belong to the later history-apply operation.

## Approval and execution

Administrators prepare a storage operation through
`POST /internal/agent/administration/store_archive/prepare`, using the standard
protocol envelope with a new nonzero UUID `operation_id` and arguments:

```json
{"sha256":"EXPECTED_LOWERCASE_SHA256","source_file_id":"CALLER_OWNED_FILE_UUID"}
```

`source_file_id` is required for the discovered agent tool. Omit it when preparing
a raw operator upload. The preparation freezes the operation, local actor, exact
SHA-256 and optional file ID for five minutes. Repeating preparation with identical
arguments returns its original ID and expiry; it does not extend approval.

The agent invokes `store_archive` only after exact-parameter confirmation. It uses
the same operation UUID as `idempotency_key` and supplies the returned proposal ID
in `confirmation.preconditions.proposal_id`, with mode `exact_parameters`.
The caller must remain a current administrator in the deployment organization and
have a nonrevoked local account. Files stay caller-owned even for administrators.
Common Telegram reads retain the 20 MiB limit; Loom receives a receipt, not bytes.

For a raw operator upload, use `POST /internal/admin/legacy/store` with bearer auth,
raw `application/gzip` body and these headers:

```text
X-Operation-Id: OPERATION_UUID
Idempotency-Key: OPERATION_UUID
X-Proposal-Id: PREPARED_PROPOSAL_UUID
X-Content-Sha256: EXPECTED_LOWERCASE_SHA256
```

The proposal must have been prepared without a file ID. Read-only `/validate`
remains read-only and does not provide a durable handle or authorize storage.
The expected digest binds content but does not authenticate a sender controlling
both bytes and digest. Real export scope and provenance still require selection.

## Atomicity, replay and limits

Migration 21 adds separate archive metadata, compressed byte chunks, preparations
and operation receipts. A single transaction writes chunks of at most 256 KiB,
revalidates the complete archive and inserts verified metadata and its receipt.
An incomplete upload has no committed archive or partial chunks. A process crash
rolls the transaction back through PostgreSQL; it does not resume from a byte offset.

After IO, the API rechecks IAM without requesting another pool connection. The
transaction then checks and locks the current local identity before committing.
Input callbacks may perform bounded Telegram IO but cannot query the same store.
This avoids needing a second connection while an upload holds a transaction.
Rollback uses a separate one-second cleanup context after request cancellation.

Reusing an operation with the same proposal and arguments returns the original
receipt without downloading or reading input again, including after approval
expires. Changed arguments or proposal conflict. A different approved operation
for identical bytes reuses the same archive and adds its own operation receipt.
Deduplication is scoped to deployment bot and organization; operation receipts
are scoped to the local actor. Different actors cannot read each other's operation
receipts through the status API.

After a lost response, call
`POST /internal/agent/administration/store_archive/status` with the same operation
UUID. A successful result returns the original receipt. `not_found` means no
committed receipt was observed, not permission to create a replacement operation
while the original request may still be running. Reuse the same operation.
The agent write route returns `rejected` with `terminal_rejection` for definitive
archive/proposal/source failures. Infrastructure and uncertain commit outcomes
remain HTTP 503 and require reconciliation. Operator upload failures retain HTTP
error codes; only the agent route uses the terminal-rejection envelope.

Storage is limited to 32 distinct archives and 1 GiB of compressed bytes per
deployment bot/organization. A PostgreSQL advisory lock serializes archive writes
for that scope, including quota accounting. The API shares the existing one-slot
validation gate per process. Streaming uploads retain the 257 MiB transport limit,
30-second context/read deadline and 31-second socket write deadline. PostgreSQL
WAL and backup space must cover archived data too; chunking limits Go buffering,
not total disk use. The inspector's existing record and line limits still apply.

Application routes provide no update/delete operation for stored bytes or receipts.
The down migration refuses populated archives or preparation history; export them
before any explicitly authorized destructive rollback. Normal backup/restore must
include all four tables. Historical data remains private and outside live business
tables. Owner-scoped history projection and avatar-byte migration remain separate
work and are not established by storing an archive.

Stored bytes can now be revalidated through the read-only `validate_stored_archive`
tool; see [decoder and reader contract](shuttle-history-decoding.md). This reads
chunks again rather than trusting the saved report and does not apply history.

## Acceptance — 2026-10-15

Architecture and Go consultations selected transactional PostgreSQL chunks before
implementation. Independent correctness, quality/usability and protocol reviews
ran in multiple rounds. They found and fixed permanent errors being returned as
unknown HTTP failures to Loom, and cancellation/read errors being misclassified
as permanently invalid archives. Fresh final reviews found no actionable issues.

Behavioral tests cover exact multi-chunk bytes, unchanged live user/task counts,
corrupt gzip tail rollback, approval expiry, changed-operation conflict, source
ownership, revocation during Telegram download, original receipt replay after
Telegram loses the file, and rollback/retry with a one-connection pool. They check
the stable receipt separately from Huma's host-dependent schema-link decoration.
The full explicit Linux application race suite passed; integration tests took
171.068 seconds. Targeted lint reported zero issues.

Our owned test stand runs migration 21 and manifest `mvp-18`. Native acceptance
used real Zitadel administrator/user tokens and synthetic archives. It verified
ordinary-user denial, source-bot mismatch rollback, SHA/operation conflict,
archive-byte deduplication, exact chunk restoration and original receipts.
A raw HTTP upload was disconnected after observing a chunk insert in an open
transaction. No partial archive became visible; the transaction released its
connection, and retrying the same approved operation then succeeded. Original
receipts and exact bytes survived a bot restart. Running the exact down SQL inside
a rollback-protected test transaction confirmed the populated-data guard.

All preexisting live business table counts and hashes stayed unchanged. A backup
of schema 20 was retained before applying migration 21. The first native probe
returned 503 because image replacement had left the schema at 20; applying the
separate `-migrate` command corrected stand preparation. No product change was
needed for that failure. Earlier failed tests and a deliberately superseded full
QA run remain recorded; the final full run is separate and passed.

Detailed evidence remains in ignored
`.cache/shuttle-live-test/native-archive-storage-checkpoint.json` and
`.cache/shuttle-work/container-results/zinusya-archive-storage-release-qa-20261015.log`.
The agent file-ID path used HTTP/PostgreSQL tests with mock Telegram; this slice
did not invoke the native Loom agent tool or send Telegram messages. No real
legacy export, historical apply, production deployment or Git publication is
claimed by this acceptance.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
