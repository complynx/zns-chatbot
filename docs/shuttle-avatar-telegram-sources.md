# Telegram avatar source lifecycle

## Contract

Telegram inputs retain their existing owner-scoped file reference, not permanent
image bytes. A draft uses exactly one source: `source_file_id`,
`history_source: {record_id, archive_id}`, or browser-upload bytes. Telegram file
IDs stay server-side and belong to the receiving bot. No source is downloaded
during historical migration. A metadata read does not establish availability.

New preview and finalization download Telegram inputs on demand, enforce the
pinned size, SHA256 and MIME type, and recheck current ownership and authority.
If Telegram cannot return the image, ask the person to resend or upload it.
Deleting a chat message alone does not determine API availability. Missing source
bytes do not invalidate successful operation receipts or stored generated JPEGs.
A source failure must not cause another operation ID or an automatic document send.
Browser uploads retain bounded bytes because they have no Telegram reference.

Migration 68 backfills source sizes before clearing Telegram-backed originals.
It preserves uploads, transforms, versions, old operation hashes/receipts and
finalized documents. Rollback refuses reference-only drafts and historical-create
operations; it does not fetch or fabricate missing bytes. Admission remains five
drafts and 50 MiB of browser uploads per owner. There is no automatic deletion or
owner discard action yet; lost-source drafts count toward this limit.

## Discovery and native use

`zinusya_history.avatar_candidates` lists at most 20 own imported file locators in
stable UUID order. `after` and `limit` control paging; follow `next_id` only when
`has_more` is true. The default limit is ten. Candidates can include unsupported
attachments. Listing neither downloads files nor validates every source archive.
`avatar_info` validates a selected complete applied archive and supported image
metadata; `avatar_fetch` also downloads. Neither creates a draft or delivery.
The general historical-content page excludes file records without projections.

`GET /app/avatars/history?after=<UUID>` exposes the same owner-only candidate list
with pages of 20. The Mini App's Find imported attachments action loads this list;
The current source requires Preview photo, then Use this photo creates a draft
through `POST /app/avatars/drafts` with
`operation_id` and `history_source`. Agent `create_draft` accepts the same locator.
Ordinary Telegram imports still use `source_file_id`, exclusively of history.
No caller supplies Telegram IDs, arbitrary URLs or another owner's identity.

The selector labels entries by page position, not historical filename or capture
time. In the current source, `GET /app/avatars/history/{archive}/{record}/preview`
downloads and renders a bounded identity-frame JPEG without creating a draft,
operation, task or delivery, or retaining original/preview bytes. Archive membership
and ownership are checked again after IO/rendering, followed by current session
and IAM validation. Responses are private/no-store. A missing source asks the user
to resend it; unsupported content is rejected. Viewing and changing candidates
clears old preview URLs. Use this photo is enabled only for the viewed candidate;
creating its draft rechecks the source independently. Better labels and explicit
draft disposal remain follow-ups.

This preview follow-up passes three Linux/PostgreSQL race scenarios: repeated
viewing leaves all draft/document/task/outbox counts at zero, foreign access makes
no Telegram request, lost sources return explicit 422, and ownership changes during
download are rejected. All 357 UI cases pass, including six previews with zero
writes and late responses after sign-out. A headless Edge fixture passes 20
DOM/canvas checks, including a decoded JPEG fitting a 320px viewport before explicit
Use, without increasing writes. Browser and loopback server are closed; this is
fixture HTTP, not real IAM/Loom or Telegram WebView acceptance of the new endpoint.
Strict lint passes. The preview follow-up is now packaged as a local QA candidate;
its binary bytes were verified and its stopped copy-probe container removed. It
is installed only on the separate `avatar-references-r1` QA stand. The bot alone
was recreated; schema 68, the other eight container fingerprints and all four
domain-table snapshots were preserved. The primary remains schema 67 / mvp-83.
A reviewed socket-deadline follow-up is now applied: this GET route receives an
80-second request/read budget and an 81-second write deadline. History listing
and POST requests to this preview URL retain their default budgets. All 15 deadline
behavior cases
pass on an isolated copy; the applied files differ only by CRLF-to-LF normalization,
verified byte-for-byte. Current strict lint passes. These checks establish the
wrapper's deadline behavior, not a real 80-second slow-download runtime test.

The full race run uses an immutable pre-deadline snapshot in sequential groups of
20 integration roots. Its result will belong to that snapshot. The later two-file
deadline change has the separate behavior/lint proof above; it is not claimed to
have passed that full race run. The original controller's working-source equality
check is expected to fail after this intentional follow-up. Its original proof
will stay intact; a separate assessment requires a passing complete test inventory,
unchanged frozen inputs, exact current/follow-up inventories and only the two
independently tested changes before accepting the frozen-snapshot evidence.

The frozen run is now complete: all 833 discovered integration roots passed in
42 groups; all packages together passed 1,148 roots with one expected watchdog
helper skip. There were no failures, unexpected skips, data-race warnings or OOM.
The observer timed out after 3,300 seconds while its original container continued.
A separately reviewed completion step collected that same container after exit 0,
without replaying tests. Its database dump was fully decoded before removing the
test container and dedicated database. The original timeout proof is unchanged;
`full-race-v2/terminal-continuation-v1.json` records completion and
`frozen-assessment-v1.json` verifies the snapshot and exact two-file follow-up.
Neither report claims full race coverage of the later working source.

The installed preview passes a separate shipped-Mini-App browser check with real
IAM/PostgreSQL and signed mock Telegram initData. Preview returns the expected
582,591-byte JPEG by SHA256 and fits a 320px viewport; foreign preview is 404.
One explicit Use creates one version-1 historical draft with no original bytes.
Users, tasks, generated documents, delivery intents, outbox rows and Loom runs are
unchanged. The own Loom container was paused during preview/use and then restored.
The browser process is confirmed exited, and both screenshots were inspected.
Evidence is under `history-preview-runtime-v1/`; primary preservation passes.
This is headless Edge with Telegram simulation, not a real Telegram WebView.

The first broad Linux race attempt is retained as failed: the integration package
was OOM-killed at its 2 GiB limit after 389 roots passed. No data-race warning was
observed, but the full gate is incomplete. Its database dump was decoded and the
temporary container/database removed. An initial preview API assertion also failed
because a JSON test helper truncated the JPEG at 64 KiB; the corrected image reader
passes, with both runs retained. The successful focused run removed its container
and database without OOM.

Native create/save/finalize operations keep their exact owner-bound request in
sessionStorage until a successful receipt or definite rejection. Recovery uses the
same operation ID. Once a save is confirmed, a failed source reload is an image
availability error, not an unknown save. A missing original closes the editor;
created documents remain listed and downloadable. Native and agent edits share
versions and reject stale writes.

## Verification boundary

The source-only reference backend passed 89 PostgreSQL scenario roots and 17
child cases with mock Telegram/IAM. The first attempt failed 9 of those roots;
its original logs and decoded dump remain preserved locally. The successful run
removed its temporary PostgreSQL container and volume and preserved the installed
primary. These are Windows non-race results before subsequent discovery/UI edits.

The updated UI passes 25 avatar-specific behavioral Node tests; all 355 tests in
the 16 UI test files also pass. These include source loss at load, preview and
finalization, receipt recovery, imported selection/paging and late responses after
owner switching. A separate real headless Edge run passes 19 DOM/canvas checks
with a fixture HTTP backend, including JPEG download after source loss and
historical-import receipt recovery. Its browser and loopback server were closed.
This is not real IAM/Loom or Telegram WebView acceptance. The first browser
attempt failed because its drag target had moved below the viewport; the fixed
fixture scrolls the real canvas into view.

The pre-refresh PostgreSQL run passes all 90 roots and 17 child cases
without skips. This includes candidate discovery with 20+2 pagination, equality
between agent/native results, owner isolation and no Telegram download. Earlier
90-root attempts each passed the original 89 roots but failed the discovery test
on fixture setup (duplicate administrator, then missing mandatory archive users).
Their logs and decoded database dumps remain preserved. All temporary PostgreSQL
containers and volumes from these runs were removed; primary preservation and
unchanged source/toolchain checks passed. Strict Go lint also passes.

Both pre-refresh Linux/amd64 executables compile. The local candidate image contains
those verified bytes; its stopped copy-verification container was removed. The
candidate runs only on a separate nine-service QA stand with real local Zitadel
and Loom, and mock Telegram. Its schema 68 / manifest mvp-85 was read back.
The primary remains on schema 67 / manifest mvp-83.

The pre-refresh Linux non-race gate with real PostgreSQL passes 95 roots across API,
DB and legacy-import packages, with no skips. Its temporary Go container was
removed; source fingerprints match and no OOM occurred. DB tests left no test
schemas or application users/tasks behind before fixture provisioning. Earlier
attempts remain recorded: a cold build exhausted a 256 MiB temporary filesystem;
a later run passed 70 roots but failed its gate because DB tests skipped without
a DSN. Those failed gates are not relabeled as successful.

On the separate stand, the shipped native upload/crop/save/preview/generate/
download flow passes against real IAM and the bot's Linux renderer while Loom is
stopped. The generated document reaches mock Telegram, and task/run counts remain
zero. Loom was restarted afterward. This checks browser-upload retention and
native independence; it does not accept historical-reference workflows. An earlier browser fixture refused the new test origin
before browser launch; its preflight log is preserved separately.

On that same mvp-85 stand, agent-only, native-to-agent and agent-to-native
flows also pass with four total drafts/documents and three completed Loom runs.
The agent-only source is a Telegram attachment reference with no retained original
bytes. During agent-to-native continuation, a normal bot SIGTERM restart exited
successfully in 0.641 seconds; the original task/run and draft survived. The form
finished the document and the agent read that exact receipt without another
render or delivery. All four documents reached mock Telegram; foreign-owner
reads were denied. These use synthetic users and a deterministic provider.

A subsequent API probe found that Loom refresh sends only external_id, while
several image methods required semantic IDs. The source now advertises mvp-86
with avatars.refetch and history.avatar_refetch adapters. Avatar locators retain
source kind and preview version; historical locators retain archive and record.
They reuse existing owner checks and read budgets. Initial methods remain
available. Existing malformed Loom references are not migrated or claimed to
recover. The reviewed mvp-86 image is now installed only on that separate stand.
Only its bot container was recreated: schema 68, all four domain-table snapshots
and the other eight container fingerprints were preserved. The primary remains
schema 67 / mvp-83.

The focused Linux/PostgreSQL refresh gate passes seven root scenarios without
skips. It obtains real HTTP descriptors, calls their advertised method using only
external_id, and checks descriptor identity and downloaded bytes. It covers all
five image families, transform changes, stale previews, foreign users/archives,
invalid locators, unavailable historical files and actual MIME differing from
untrusted metadata. Its temporary test container was removed without OOM. This
is API acceptance, not proof that Loom itself refreshed an expired cached file.
Strict lint passes after final line wrapping; independent code and documentation
reviews have no remaining findings for this adapter change.

The installed mvp-86 runtime passes real Loom import/open/files.info for current
original, preview and finalized descriptors. For each family, forced artifact
metadata expiry causes a new physical artifact to be imported with the same
MIME, size and SHA256. Repeating expiry then reading through the original
run-scoped handle succeeds too. This tests artifact expiry, not handle TTL
expiry or physical cache-file deletion.

After the saved draft changes, the expired old preview returns the exact Loom
error `module HTTP status 409`; the source API also reports a version conflict.
The new preview imports and opens successfully in that same run. The old
reference remains unchanged, the reactive task completes, and the four generated
documents/delivery intents are unchanged. Telegram-backed drafts still retain no
original bytes. A fixture assertion initially rejected its own intentional error
field; its checkpoint is preserved, and the completed result was reconciled
without repeating the script or draft mutation. The first packaging attempt also
retains its failed Docker FROM/image-ID log; the corrected build uses a verified
local tag, verifies the packaged binary and removes its copy-probe container.
Independent controller and runtime-proof reviews have no remaining findings.

Subsequent historical-reference acceptance on that same stand passes both
native/agent handoffs, real Loom historical import/refetch after artifact expiry,
and source-loss handling with completed JPEG access retained. The archive contains
only references, and historical drafts retain no original bytes. Exact crop,
source, draft/version, current owner and generated JPEG bytes are checked. See
[historical runtime QA](shuttle-avatar-history-runtime-qa-2026-10-21.md) for evidence,
fixture failures, review results and synthetic-data limits.

The single-step `generate` path also passes on the same mvp-86 stand. One new
synthetic media request runs through actual Loom generation, semantic `fetch`,
and plain-UUID `refetch` after artifact metadata expiry. The expected current
owner is checked before expiry is changed. The resulting JPEG matches the
separate-process identity render, and the actual mock Telegram recipient,
message receipt and downloaded bytes match it. Comparing all document messages
before and after proves exactly one new send; one avatar outbox row exists.
Immediately after that single-step check there were seven generated documents,
six drafts and eight completed tasks. The later preview/use check adds one draft.
The single-step delivery uses the task outbox; the six draft delivery intents
are unchanged. No Telegram original is retained. Evidence is in the ignored
`legacy-avatar-migration-v1/legacy-generated-runtime-v1/runtime-acceptance.json`;
its two-pass fixture review closed one duplicate-send acceptance gap. The primary
stand preservation check passes. This uses deterministic model responses and
mock Telegram, and does not prove handle TTL expiry or real Telegram WebView.

The frozen-source race gate and separate two-file deadline checks are complete.
Full race coverage of the later source and actual Telegram WebView acceptance
remain separate open gates. This image is not installed on the primary and
production cutover is not authorized.

## Later archive-validation budgeting (source only)

Ordinary browser uploads and current Telegram originals do not acquire the global
archive-validation slot. Their reads, previews and finalizations remain available
while an unrelated archive validation is admitted. Historical source reads and
new create/finalize commits use the slot; it is released before Telegram IO,
rendering or response authorization. Each archive scan has its own 30-second
budget within the broader request deadline. Complete archive integrity and current
ownership checks remain mandatory at commit.

Completed create/finalize requests recover their existing receipt before acquiring
an archive slot. The normal current owner/session checks still run. A busy slot
returns 503 without persisting a draft or generated document; retry uses the same
operation ID.

Seven focused Linux/PostgreSQL race roots pass, including four historical
create/finalize agent/native cases. HTTP tests hold an admitted archive upload
open and separately occupy it between avatar rendering and commit. They verify
ordinary upload/current-Telegram access, rejection without effects, successful
retry and receipt replay while the validator is busy. Older single-connection,
receipt-race and migration compatibility cases also pass. There are no skips,
data-race warnings or OOM; the dump is decoded and temporary container/database
removed. Strict lint passes. Initial lint/format and test-compilation failures
remain in the ignored `archive-backpressure-v1/` logs; the successful race proof
is `archive-backpressure-pg-v1/proof.json`.

This later source fix is not part of the earlier full frozen-source race run or
the installed QA image. Full current-source CI, new packaging/runtime acceptance
and primary installation remain separate gates.
