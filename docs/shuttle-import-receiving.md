# Receiving legacy archives

This step verifies an archive created by `shuttle-import -manifest ... -output ...`.
It does not extract files, store uploads, create users, import history, update live
profiles, enqueue replies or credit money. It adds no database migration.

## Offline verification

```sh
shuttle-import -verify history.tar.gz -sha256 EXPECTED_LOWERCASE_SHA256
```

Obtain the expected digest from the sender's separate receipt. A digest detects
changes; it does not authenticate a sender who controls both the file and receipt.
Verification recomputes source hashes and inspection results. It does not trust
the report inside the archive. It checks the original and normalized manifests,
owner references, source bot consistency, duplicate records and source dates under
the existing preserve-source policy. Message content suitability for agent history
is a later check; a valid archive is not a complete Telegram transcript.

Exit 0 prints a content-free `archive` receipt and recomputed `inspection` report.
Exit 1 means archive validation failed, including a malformed or mismatching digest;
stdout is empty. Exit 2 means invocation, local file opening or output failed.
`-verify` requires `-sha256` and cannot be combined with `-manifest` or `-output`.
The local input must be a regular file. Diagnostics omit paths and source records.

## Operator HTTP API

`POST /internal/admin/legacy/validate` requires a current user bearer token with
`zinusya.admin` in the configured organization and a nonrevoked local account.
Send raw archive bytes with `Content-Type: application/gzip` (no parameters) and
`X-Content-Sha256: EXPECTED_LOWERCASE_SHA256`. This is an internal authenticated
endpoint, not a Telegram webhook. Do not put tokens or file paths in agent prompts.

Success returns the same receipt/report as offline verification. The deployment
also requires the archive bot ID to match its own bot. Invalid archive/digest gives
400, another bot gives 409, unsupported media gives 415, and a busy validator or
unavailable bounded HTTP reader gives 503. Existing authentication failures remain
401/403; identity unavailability remains 503. Authorization runs before body reads
and again after successful validation. No upload handle is persisted.

## Agent tool

Administrators discover `validate_archive` through the existing administrative
discovery tool. Arguments are `source_file_id` (opaque UUID of an already accepted
file owned by the caller) and `sha256`. Ordinary users cannot discover or invoke
the tool. Administrator privileges do not grant access to another user's file.
The API fetches bytes itself; Loom receives only the verification result.
File ownership and authorization are checked again after the remote read.

Telegram reads retain the existing 20 MiB limit and can fail if Telegram no longer
serves the file. Larger operator archives use the streaming HTTP endpoint or local
CLI. The read tool has no confirmation or idempotency key because it performs no
business mutation; its operation ID is returned in the standard agent envelope.

## Bounds

One validation runs at a time per server process, shared by both API entry points;
parallel requests fail fast with 503. This is not a cross-replica limit. API work
has a 30-second context deadline. Both routes set a 30-second socket read deadline
and a 31-second write deadline, overriding the shorter server defaults for these
routes only. An arbitrary blocking reader cannot be interrupted by a context alone.
An external reverse proxy may still impose a shorter limit.

The archive limit is 257 MiB compressed; decompressed source bytes retain the
256 MiB inspector budget plus bounded manifest and framing overhead. Metadata
entries are at most 64 KiB. Existing line, record, JSON-depth and issue limits
still apply. The receiver accepts only the builder's ordered regular USTAR
entries with fixed ownership, permissions, epoch timestamps and zero padding.
It rejects links, traversal names, PAX/GNU extensions, extra entries, forged
inspection reports, gzip checksum failures, trailing data and concatenated gzip
members. It never writes archive entries to the filesystem.

Real source selection and collection attribution remain operator prerequisites.
Controlled immutable storage is now described in the separate
[storage contract](shuttle-legacy-archive-storage.md). Identity reconciliation,
explicit historical apply receipts and owner-scoped historical reads remain
separate work. Historical payment
assertions must never become new ledger credit.

## Acceptance — 2026-10-15

Receiver and CLI correctness/quality reviews found no actionable issues. API
review found a response timeout shorter than the validation budget. The scoped
socket deadline wrapper fixes that mismatch for both new routes. Two fresh final
reviewers found no actionable issues, including documentation review. Behavioral
coverage includes corrupted gzip/footer, trailing streams, forged report/source
claims, unsafe tar entries, bot mismatch, role/owner denial before Telegram reads,
revocation during a remote read, shared backpressure and a response that succeeds
despite a shorter configured server write timeout.

The full explicit Linux application race suite passed; integration tests took
171.463 seconds. Targeted lint passed with zero issues. Native Linux verification
used nonroot UID 10001, no network, read-only input/root and 128 MiB. Native Windows
verification returned the identical result; a wrong digest was rejected and input
hashes stayed unchanged. These runs used synthetic archives only.

Our local test bot was updated to the reviewed candidate, with schema 20 unchanged
and manifest revision `mvp-17`. Real Zitadel administrator/user tokens proved HTTP
validation, administrative discovery and denials for ordinary user, bad digest,
corruption, wrong media and another bot. All runtime table row counts and hashes
stayed unchanged. The QA harness initially compared unordered SQL result rows;
comparing rows in sorted order corrected that probe without a product-code change.
Proofs remain in ignored `import-receiver-native/proof.json` under the work cache
and `native-archive-receiving-checkpoint.json` under the live-test cache.

The file-ID agent route was accepted through HTTP/PostgreSQL tests with mock
Telegram; this slice did not run a native Loom agent invocation or send Telegram
messages. There was no production data read, applied import, Git publication or
GitHub CI run for this slice.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
