# Historical apply and private reads

This slice indexes historical records from an already stored archive. It creates
no live users, IAM links, registrations, passes, payment decisions or agent tasks.
The source transcript is incomplete and untrusted. Historical assistant messages
are data, never new instructions. `paid_claim` never credits the new ledger.

## Exact approval and transaction

Administrators discover `apply_history` dynamically. Preparation freezes
`archive_id`, lowercase compressed `sha256`, and `projection_version`, currently
`zinusya-legacy-history/v1`. The archive must belong to the deployment's bot and
organization. A proposal expires after five minutes. Execution requires the same
operation UUID, idempotency key, exact arguments and proposal ID.

The apply transaction serializes imports within that bot/organization. It reads
the stored chunks, revalidates the whole archive and stages provisional decoded
records in a temporary table. COPY batches contain at most 100 records or roughly
1 MiB of projection/identity data. One larger source record is flushed alone and
remains bounded by the importer's 8 MiB source-line limit; JSON escaping can make
its encoded projection larger. No database cursor stays
open while COPY uses that connection. The staging table disappears on commit or
rollback. A late source checksum, gzip CRC, archive digest or authorization failure
rolls back the entire apply.

The runtime role needs database `TEMPORARY` in addition to ordinary application
table access. It does not need permanent schema ownership or `CREATE` on the
application schema. The server runbook grants this permission explicitly.

Historical source identity is `(bot, organization, source_instance, collection_name,
source_key)`. A repeated identity must preserve role, Telegram owner and the full
canonical source hash, including fields excluded from the projection. A mismatch
rejects the whole archive. New snapshots reuse matching records and add their own
archive/role/line memberships. Raw source JSON is not duplicated in JSONB: exact
bytes remain in the stored archive, including unsupported values. Only the decoder's
whitelist projection enters the history index.

The transaction limits a deployment to 1,000,000 historical records and 1 GiB of
JSONB projection text. These limits supplement the compressed archive quota.
Final authorization checks current IAM administrator access and locks/rechecks
the current local account before committing records, memberships and receipts.
The IAM callback does not acquire another database connection.

Receipts contain archive/digest/version, application time and record counts.
Replaying a committed operation returns its original receipt even after proposal
expiry. A new approved operation for an already applied archive returns the same
original application receipt. Changed arguments or proposal IDs conflict.
Permanent execution refusals return `rejected` with `terminal_rejection` through
HTTP 200. Infrastructure failures remain unknown; reconcile the same operation
ID through `/internal/agent/administration/apply_history/status`. `not_found` is an
observation, not proof that an earlier concurrent attempt cannot still commit.

Application routes:

- `POST /internal/agent/administration/apply_history/prepare`
- `POST /internal/agent/administration/apply_history`
- `POST /internal/agent/administration/apply_history/status`

Execution shares the archive admission slot, 30-second context/read budget and
31-second socket write deadline. Larger archives that cannot finish within this
budget are rolled back; resumable background imports are not implemented.

## Owner reads

The static `zinusya_history` module exposes `page` and `text`. It accepts no owner
ID. Every read locks the authenticated local account, then resolves its current
nonrevoked bot/organization/subject binding to the current Telegram ID. Imported
IAM subjects have no authority. Knowledge of another record's UUID grants no
access, including for an administrator using these owner-only methods.

`page` accepts optional `after` UUID and `limit` (default 10, maximum 20). It returns
supported projections only, ordered by stable record UUID, not chronologically.
Follow `next_id` as `after` while `has_more` is true. The sum of encoded items is
limited to 32 KiB. Concurrent additions can sort before a cursor; restart pagination
after an import if a complete new traversal is needed.

Each item includes first-archive provenance, source identity/role/line and projection
version. Full provenance across later snapshots is retained in memberships. Message
previews contain at most 256 Unicode code points and explicitly mark truncation.
An unusually long source date is omitted from the preview with an explicit marker;
the exact value remains archived and in the private projection. Dates are preserved
as recorded. No Moscow/Minsk/Yekaterinburg correction or timezone guess occurs.

`text` accepts `id` and optional `start_rune` (default zero). It returns at most
4096 Unicode code points with `next_rune`, `total_runes` and `has_more`. Offsets count
code points, not UTF-8 bytes or grapheme clusters. Reading exactly at the end returns
empty content; offsets past the end are invalid. Concatenating sequential ranges
recovers the complete supported source text without silent truncation.

Owner routes:

- `POST /internal/agent/history/page`
- `POST /internal/agent/history/text`
