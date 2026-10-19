# Verified offline snapshot archives

The optional archive mode preserves a validated legacy snapshot before any
runtime import is attempted. It extends [the snapshot inspector](shuttle-import-preparation.md)
and uses the same manifest, ownership, checksum, date-shape and resource limits.
It creates no users, tasks, registrations, passes, payment records or IAM links.
It does not fetch avatar bytes or expose historical records to the agent.

## Operator use

Build the separate operator CLI with Go 1.27.2 and supply a new destination:

```sh
go build -o /tmp/shuttle-import ./cmd/shuttle-import
/tmp/shuttle-import -manifest /private/snapshot/snapshot.json \
  -output /private/archives/legacy-v1.tar.gz > /private/archive-receipt.json
```

Without `-output`, behavior remains inspection only. The destination parent must
already exist, support hard links, and be controlled by the operator. Source and
destination directory entries must not be changed by untrusted concurrent writers.
The source snapshot should remain immutable. No live database credentials are
needed. The bot Docker image does not include this operator executable.

The archive deliberately contains private original data, including unknown legacy
fields. It is not a redacted export. Use a private output directory. POSIX archive
files use mode `0600`. On Windows, privacy depends on the destination directory's
ACL; Go file mode bits do not establish a private ACL. Keep source snapshots and
archives out of Git, public downloads, logs and agent context until a separately
reviewed owner-scoped history interface exists.

## Archive and integrity

Format `zinusya-legacy-snapshot/v1` contains only these regular tar entries:

- `bundle-manifest.json`: the validated manifest with collection order normalized
  to users/messages/passes/files and paths changed to `source/<role>.ndjson`.
- `original-manifest.json`: the exact original manifest bytes read by the tool.
- Present `source/<role>.ndjson` files: the exact source bytes, including line endings.
- `inspection-report.json`: validation of those archived source bytes.

The original manifest is read once. Each source is opened once: the same read
stream feeds its checksum, NDJSON validation and tar entry. A changed source that
does not match the expected checksum, or does not fill its declared archive entry,
cannot publish a valid bundle. This binds validation to the archived bytes; it
does not monitor later changes to the original files.

Tar entry order, regular-file modes, owner metadata and timestamps are fixed.
The gzip header has no filename or wall-clock timestamp. Identical snapshot bytes
produce identical archive bytes with the same tool build. Different original
manifest whitespace or ordering intentionally changes the archive: the original
manifest is preserved exactly. Cross-version compressor identity is not promised.
Extraction into a private directory followed by inspection of
`bundle-manifest.json` validates the preserved snapshot again. The command does
not provide a general archive extractor.

The command writes a private temporary file in the destination directory. It
publishes the final name only after validation, tar/gzip finalization, file sync
and close succeed. Publication uses an exclusive hard link: existing files,
symlinks and another publisher's result are preserved. Unsupported hard-link
filesystems fail without a replacing-rename fallback. There is no implicit overwrite.

## Result and failures

On success, stdout contains `inspection` and `archive`. The latter contains
`format`, `sha256` and `size_bytes`. Neither the receipt nor diagnostics contain
source records, owner IDs, file references or raw filesystem paths. Inspection's
`import_performed` and `complete_telegram_history` remain false.

Exit 0 means a verified archive was published. Exit 1 means validation failed:
stdout retains the inspection issues, there is no archive receipt, and no final
archive is published. Once validation fails, inspection can continue for its
report while archive writing stops. Normal failures remove the temporary file.

Exit 2 means invocation, manifest, archive I/O, publication or output failure.
A temporary-file deletion failure additionally emits `cleanup_pending: true`
and a generic warning to inspect the output directory. If publication had already
succeeded, the archive receipt is retained; that file must not be treated as absent.
If validation failed, the same cleanup warning can describe a residual unpublished
temporary file. Check the receipt and destination before retrying. Stdout failure
can also happen after successful publication.

Abrupt process termination can leave a private `.zinusya-legacy-*.tmp` file.
After confirming no archive command is active, inspect and remove that specific
temporary file from the operator-controlled output directory. A temporary file
is never promoted as a valid archive during restart or retry. This CLI does not
claim cleanup after SIGKILL or power-loss durability of directory metadata.

## Acceptance — 2026-10-14

Architect and Go consultations agreed the single-stream design before development.
Behavioral tests cover exact raw bytes and source manifest, round-trip inspection,
repeatability across directories, private-data exclusion from receipts, changed
source checksums, rejected oversized input, existing destination preservation,
temporary cleanup and exactly one concurrent publisher.

Independent correctness, quality/usability and filesystem/privacy reviews ran in
two rounds. They found and fixed validation-limit failures masking the inspection
report and silent temporary cleanup failures. A regular-file check was also added
before opening a manifest. All three fresh final reviewers reported no actionable
findings. Focused Linux race tests passed for the CLI and full inspector package;
lint reported zero issues. This adds no dependencies or runtime schema/API changes.

Native Linux acceptance used nonroot UID 10001, no network, a read-only root and
source mounts, a private `0700` tmpfs output directory and a 128 MiB memory limit.
It checked mode `0600`, exact extracted bytes, reinspection, same-byte rebuilds,
bad-input refusal, read-only output failure, existing file/symlink preservation
and no residual temporary files. Native Windows NTFS acceptance checked hard-link
publication, unchanged existing destinations, repeatability and concurrent writers
in a new private output directory with owner/SYSTEM/Administrators ACL only.
All original input hashes stayed unchanged.

The synthetic test archive was 1087 bytes with SHA-256
`b770d575726b55c3a65b9e760570baef1784cc73027139b4538612b0ff58cc5c`
on both tested platforms. A separate real NTFS delete-denied test proved cleanup
warnings for both published and validation-rejected archives. It preserved the
published receipt, left rejected input unpublished, and then restored the QA ACL
and removed all residual test files. Detailed proofs remain in ignored
`.cache/shuttle-work/import-bundle-native/proof.json` and `cleanup-fault-proof.json`.

Receiving validation is described separately in the
[receiver contract](shuttle-import-receiving.md). The acceptance above concerns
archive creation only.

Only synthetic local exports were used. There was no production data read, live
IAM/Telegram operation, bot image rollout, applied data migration or GitHub CI run
for this slice. Controlled archive loading, identity reconciliation and owner-scoped
historical reads are next; historical payment claims must never become ledger credit.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
