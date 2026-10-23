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

Native create/save/finalize operations keep their exact owner-bound request in
sessionStorage until a successful receipt or definite rejection. Recovery uses the
same operation ID. Once a save is confirmed, a failed source reload is an image
availability error, not an unknown save. A missing original closes the editor;
created documents remain listed and downloadable. Native and agent edits share
versions and reject stale writes.

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
