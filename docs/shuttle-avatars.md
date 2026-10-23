# Framed avatar documents

The `zinusya_avatars` module restores the legacy fixed-frame generator. After an
explicit user request, it takes an existing owner-scoped `source_file_id`, makes
a 1024×1024 JPEG with the ZNS frame (centered by default), and queues that document to
the owner's private Telegram chat. It does not set a Telegram profile photo or
introduce a separate current-avatar field. Explicit agent generation remains
available alongside the native image workflow below.

## Native image entry

A single captionless Telegram photo, or a document with an `image/*` MIME type,
uses the native avatar path when it is not a reply and the conversation has no
active user question. Captions, replies and images answering an active question
keep their conversational purpose. PDF documents do not enter this path. MIME
admission is not image validation: the renderer still accepts only bounded JPEG
or PNG bytes; unsupported or unavailable sources produce native guidance.

The system authenticates the current owner, creates a shared identity-transform
draft and finalizes its framed document through durable operation receipts.
It starts no Loom run and makes no model call. Repeated processing recovers the
same draft/finalization operations. Rendering happens outside database locks;
commit rechecks the owner and atomically saves the artifact and delivery intent.
Existing quotas and source-access checks still apply.

When the Mini App is available, the document includes a localized Crop button
for its draft, opening `#avatar-crop?draft=ID`. Finalization success means stored
and queued. Only an acknowledged Telegram send establishes delivery and that
the Crop button was offered. An unknown send requires reconciliation and does
not automatically resend. Offering the button does not establish that it was
opened; an authenticated explicit Crop entry records that separately. Later
agent context carries these facts as owner-scoped history, never fresh consent
to generate or send another document.

## Telegram source references

The agreed source policy is to retain Telegram file references and ownership,
then download the source when requested. Migration must not download and archive
all historical originals. If Telegram no longer returns a file, show a clear
error and ask the person to resend it. Message deletion alone does not establish
file availability; use the actual Telegram API result. File identifiers stay
server-side and belong to the bot identity that received them.

Historical input images are read through `zinusya_history.avatar_info` and
`avatar_fetch`, with `record_id` and `archive_id` from
`zinusya_history.avatar_candidates`. The general `history.page` contains projected
historical content and does not expose files with no content projection. `avatar_info` validates the full applied archive and
returns source metadata without a Telegram request. `avatar_fetch` downloads on
demand, rechecks ownership/IAM, and returns an authenticated file descriptor.
The descriptor's `fetch_method` is `avatar_refetch`, which
accepts only its exact `external_id` (archive and record locator). Its content path is
`/internal/history-avatars/{archive_id}/{record_id}/content`. Downloads repeat
source authorization before and after remote IO and refresh the Telegram path.
Telegram identifiers are excluded from returned JSON. Neither method creates a
draft, task, account or delivery, and metadata does not establish availability.
Info validation has a 30-second budget; fetch/content allow 80 seconds for two
validations and Telegram IO, with matching socket deadlines. A failed download
returns an error; the agent can retry or ask the person to resend the photograph.

## Agent and API contract

All tools use `POST /internal/agent/avatars/{method}` and the usual authenticated
agent envelope. `generate` requires a nonzero UUID operation ID, the same
idempotency key, `arguments.source_file_id`, and the current owned `run_id`.
An optional `arguments.transform` contains four integer fields: `offset_x_ppm`,
`offset_y_ppm`, `zoom_ppm` and `angle_millidegrees`. One million PPM is one output
square width or identity zoom. Offsets range from -65,536,000,000 to
65,536,000,000; zoom from 250,000 to 8,000,000; angle from -180,000 to 180,000.
Positive offsets move the photo right/down; positive angles rotate clockwise.
Explicit zero zoom rejects. Omission is identity; +180 degrees canonicalizes to
-180 before operation hashing. Explicit identity preserves existing operation
hashes, renderer v1 metadata and JPEG bytes. Edited receipts add the canonical
transform and identify renderer v2. Changing a transform under the same
operation ID rejects.

The server resolves the active bot task and private recipient. Agents cannot
supply a chat ID, local path, executable path, frame URL or arbitrary image URL.
The source may belong to an older completed task; the delivery belongs to the
current task. A new generation requires that task's active conversation binding.

When Loom starts before the bot has persisted its create response, generation
waits with a retryable service response. It downloads nothing during this gap.
A later retry uses the same operation ID and arguments. Unknown or foreign runs
without an owned pending create cannot authorize generation. The pending state
never authorizes a write itself.

The immutable generation receipt contains artifact/source/task/delivery IDs,
source and output hashes, frame hash, renderer version, dimensions, size and
creation instant. Success means generated and durably queued. `operation`
returns this original receipt for reconciliation, even after the run completes
or the Telegram source disappears. It accepts the original operation ID in the
envelope, or `arguments.operation_id` for an independent read-tool invocation.
Changed arguments under an existing operation ID reject. Current owner,
organization, bot, subject and revocation are checked before returning a receipt.

`get(artifact_id)` returns `{avatar: receipt, delivery_state: current_state}`.
`fetch(artifact_id)` returns the original metadata and an authenticated JPEG
descriptor for Loom's file tools. `/internal/avatars/{id}/content` serves the
stored bytes with attachment, nosniff, private/no-store and sandbox headers.
Administrators have no arbitrary-owner read bypass.

## Processing boundary

`zinusya-avatar` is a small companion executable built from this repository.
The bot supplies bounded source bytes through stdin and reads a bounded JSON
result through stdout. Its environment contains only explicit runtime settings;
server credentials are not inherited. No user-controlled command arguments or
paths cross this boundary. One child runs at a time per processor instance.
Cancellation and a three-second processing deadline kill the child. HTTP
handlers inherit the application's cancellation context during shutdown.

Inputs are JPEG or PNG, at most 20 MiB, at most eight million declared pixels,
and no dimension above 8192. Dimensions are checked before full decoding.
EXIF orientation is applied first. Identity uses the original center crop and
Lanczos resize. Edited images use centered cover fit, uniform zoom, clockwise
rotation about the oriented photo center, then output-frame translation.
Shrinking is prefiltered with Lanczos before inverse affine sampling; the
prefilter never enlarges the source. The destination remains 1024×1024 even for
extreme source aspect ratios or offsets. Uncovered regions are white. The trusted project frame is composited, transparency flattened onto
white, and a metadata-free quality-100 JPEG is emitted. Output is at most 4 MiB;
the JSON protocol is at most 6 MiB and retained stderr at most 4096 bytes.
The fixed frame hash, processor version and exact canonical transform are
verified before persistence. Identity still sends raw JPEG/PNG stdin. Edited
requests prefix the original bytes with a strict JSON line containing
`protocol_version: 2` and `transform`; the JSON line is at most 1024 bytes before
its newline. Input image bytes are not base64 encoded.

The image library is linked only into the leaf renderer, not the bot's process
adapter. There is no face detection, Python, Torch, native image runtime or
general plugin framework in this slice. See the [dependency policy](shuttle-dependencies.md).
`GOMEMLIMIT=128MiB` is a soft child heap target. The Docker memory limit remains
the hard bound shared by the bot and child; do not describe this as a separate
kernel memory sandbox.

## Durability and delivery

Rendering runs outside database locks. Afterward, a transaction rechecks the
source owner, operation identity and current run, then stores the JPEG,
provenance, receipt and document outbox intent together. A precommit crash can
repeat computation; it cannot expose a second committed artifact or send intent
under the same operation. Receipt lookup precedes remote reads on retries.

The same outbox handles text and avatar documents. Both allocate their sequence
under the parent task lock, so simultaneous agent replies and generated files
do not collide. Delivery checks the live account before uploading bytes using
Telegram `sendDocument`. A checked Telegram receipt marks delivery successful.
An ambiguous send becomes `delivery_unknown` and does not automatically resend.
Existing task diagnostics remain available. For an uncertain outcome, explicit
local closure does not regenerate the image or establish Telegram delivery.

Generated JPEGs remain in PostgreSQL independently of Telegram and Loom caches.
The legacy generation path does not retain source bytes: later recropping after Telegram source loss is
not promised. The module has a hard storage ceiling of 20 artifacts per owner.
It rejects further new generations when full, while existing receipts/fetches
remain usable. Task-free finalization/delivery are implemented in later APIs.
Automatic retention, deletion UI, alternative/event frames, face processing and
production historical-avatar import acceptance remain later work. This ceiling is a bound for the current slice, not a production retention
policy. Record production retention decisions before cutover.

## Build and rollback

Migration 20 adds `shuttle_avatars`. Apply it with the normal `-migrate` command
before starting the candidate. The Docker build installs both executables.
For a manual Linux build, build `./cmd/shuttle` and `./cmd/zinusya-avatar`, then
configure the trusted absolute `avatar_executable` path. Its default is
`/usr/local/bin/zinusya-avatar`; `SHUTTLE_AVATAR_EXECUTABLE` overrides YAML.
The companion must exist at startup. Credentials and business policies do not
belong in its command line.

Before returning to an older binary, stop new generation and drain or explicitly
resolve document deliveries.
Older binaries do not understand typed document
outbox payloads.
Keep the additive media table, backups and receipts.
Migration
rollback first takes an exclusive NOWAIT table lock, rejects active use, and
refuses to drop a populated avatar table; export and retain its bytes
and history before any deliberate destructive removal.

The [shared draft API](shuttle-avatar-drafts.md), [saved-draft preview](shuttle-avatar-preview.md)
and [native editor](shuttle-avatar-editor.md) use the same versioned editing state.
