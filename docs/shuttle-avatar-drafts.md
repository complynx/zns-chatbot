# Shared avatar edit drafts

Schema 55 introduced shared drafts. Source schema 68 / manifest `mvp-86` changes
Telegram-backed drafts to references; see the [current source lifecycle and QA
boundary](shuttle-avatar-telegram-sources.md). The owned primary now runs
schema 68 / mvp-86; see the [installation evidence and remaining acceptance
limits](shuttle-avatar-primary-qa-2026-10-21.md). Earlier stage records retain their
original scope.

## State and authorization

Each draft pins one JPEG/PNG source reference or browser upload, its actual digest, verified
content type, size, frame digest and canonical transform. Versions start at 1.
Another image creates another draft. Metadata contains no image bytes, Telegram
file credentials or session tokens. Originals retain their original metadata,
including EXIF if present; only generated framed JPEGs strip image metadata.

No conversation, task or active Loom run is required. Agent calls use the
current user token. Native calls use the existing Mini App origin, current IAM
identity and session. Native writes recheck owner identity, bearer generation,
expiry and revocation inside the same transaction as the draft and receipt.
UUIDs locate data and grant no access. Administrators have no arbitrary-owner
read or write bypass.

A successful Telegram import pins metadata without retaining original bytes.
Metadata, saved transforms and successful operation receipts survive Telegram
source loss and Loom downtime. Preview and new finalization require a fresh source
download; unavailable photos produce resend guidance. Browser uploads retain bytes.
The existing `generate` method remains a separate legacy path: it still needs
an active owned run, queues its task document and does not pin source bytes.
Existing generation receipts, operation hashes and artifact tables are unchanged.

## Commands and retries

Agent tools use `POST /internal/agent/avatars/{method}`:

- `create_draft`: exactly one `arguments.source_file_id` or `arguments.history_source`; pin an owned Telegram input.
- `draft`: `arguments.draft_id`; read current metadata.
- `drafts`: empty arguments; list at most five retained drafts, newest edit first.
- `save_draft`: `draft_id`, `expected_version` and a complete `transform`.
- `draft_operation`: `arguments.operation_id`; read the original successful result.
- `fetch_original`: `draft_id`; return a file descriptor for Loom file tools.

Write envelopes require a nonzero UUID `operation_id` and matching
`idempotency_key`. Read envelopes omit the idempotency key. Draft commands do
not require or hash a run ID. Source-file imports resolve a successful receipt
before contacting Telegram. Upload identity derives from the bytes received by
the server; a client-provided digest cannot establish replay.

A save uses the [transform contract](shuttle-avatars.md): frame-relative PPM,
clockwise millidegrees, bounded values and canonical half-turn normalization.
It succeeds only for the exact current draft version, then increments it.
Concurrent form/agent edits have one winner. Reread after a version conflict;
do not silently overwrite the other channel. An identical operation returns its
immutable original result before checking the current draft version. A changed
command, payload or expected version under that operation ID conflicts. These
rules are shared across transports and survive application restart.

Native routes under the existing Mini App base URL:

- `POST /app/avatars/drafts`: JSON `operation_id` and exactly one `source_file_id` or `history_source`.
- `POST /app/avatars/drafts/upload`: raw image body and UUID header
  `X-Zinusya-Operation-Id`; image/jpeg, image/png or application/octet-stream.
- `GET /app/avatars/drafts` and `GET /app/avatars/drafts/{id}`.
- `POST /app/avatars/drafts/{id}`: JSON `operation_id`, `expected_version`, `transform`.
- `GET /app/avatars/operations/{operation}`.
- `GET /app/avatars/drafts/{id}/original`.

The binary upload route alone accepts at most 20 MiB. Existing JSON routes keep
their 64 KiB limit. The isolated leaf validates the actual image before commit;
caller MIME labels do not authorize content. Known oversized Telegram sources
retain their HTTP413 rejection before remote I/O. Source reads and processing occur
outside database locks. Commit rechecks authorization, ownership and quotas.
Original downloads are authenticated attachments with nosniff, sandbox CSP and
private/no-store headers. Agent descriptors use
`/internal/avatar-drafts/{id}/original`; Loom receives IDs and can read bytes
when needed without owning long-lived file storage.

## Bounds, action history and rollback

Admission is bounded to five retained drafts and 50 MiB of browser-upload bytes
per owner, with 20 MiB per source image. Telegram references do not consume the
retained-byte quota, but they count toward the five-draft limit. Owner serialization protects count and byte limits against
parallel requests. These are compile-time evaluation safeguards, not production
retention policy. No silent expiry or deletion is implemented. Quota exhaustion
rejects new drafts; existing reads, edits and operation replay remain available.

Successful native creates and saves are journaled atomically with their receipt.
The next reactive agent task receives at most five historical native operations
that preceded it. This metadata is explicitly untrusted history, not current
intent or consent to generate/send. Native actions do not start agent work.

Migration55 adds two avatar-local tables and the composite file-owner provenance
constraint. Down acquires exclusive table locks with NOWAIT before inspecting
history; active writers make rollback fail safely. Nonempty drafts or operation
history also reject rollback. Export originals and receipts before any deliberate
removal. This guard does not establish safety of every historical migration.

See [draft source acceptance](shuttle-avatar-drafts-qa-2026-10-19.md). The [authoritative preview API](shuttle-avatar-preview.md) is implemented. The [native visual editor](shuttle-avatar-editor.md) is implemented;
the later [finalization API](shuttle-avatar-finalization-api.md) stores immutable
results and independent delivery intents. Acceptance records describe their own
revisions. The [Telegram reference change](shuttle-avatar-telegram-sources.md) has
separate current verification requirements. Production retention/discard policy remains open.
