# Native avatar editor

The embedded Mini App edits shared drafts. Telegram originals are downloaded
on demand through the [source lifecycle contract](shuttle-avatar-telegram-sources.md).

## Use

Open the existing Mini App account page and find Avatar. Select a retained draft,
or explicitly upload a new JPEG/PNG original, at most20 MiB. Existing evaluation
admission limits remain five drafts and 50 MiB of browser uploads per owner; no automatic deletion
policy is inferred.

For imported historical photos, Find imported attachments lists owned candidates.
Preview photo shows a bounded framed image without spending a draft slot. Use
this photo explicitly creates the shared draft. Changing the selection or leaving
the account clears the preview.

Drag the photo, or enter frame-relative horizontal/vertical percentages, scale
25–800%, and clockwise rotation −180–180 degrees. Reset returns local edits to
the identity crop. Save edits writes the complete canonical transform with the
currently loaded draft version. It does not generate a durable artifact or send
anything to Telegram. Local canvas rendering is approximate: after saving,
Preview saved version fetches the authoritative server JPEG for that exact
version. A changed draft rejects its old preview path.

The frame and original are fetched from the bot by ID and checked against the
saved digests. Browser image decoding follows EXIF orientation. The browser's
interpolation is not claimed to match the leaf renderer pixel for pixel.
The server remains responsible for bounded image admission and processing.

## Dedicated crop view

The document's Crop button opens `#avatar-crop?draft=ID` directly on its owned
shared draft. This full-screen canvas carries the interaction style of the
original Python application's frame editor. It uses Telegram theme colors,
stable viewport height and safe-area variables, with browser fallbacks. Native
Telegram MainButton and BackButton controls are used when available; ordinary
page buttons remain the browser fallback.

Drag to move the photo. Two fingers move, zoom and rotate it together. With a
mouse, scroll zooms, Shift-scroll gives finer zoom, and Shift-drag rotates. The
same canonical transform bounds apply as in the generic editor. The local canvas
is approximate and does not replace the server renderer.

Finish saves a changed transform with the loaded version, obtains the saved
version's server preview, then finalizes that exact draft version. These are
separate recoverable operations. It does not finalize while a save is unknown,
and an already generated unchanged version cannot be submitted again from this
view. Successful finalization shows that the document is queued, not delivered.
The generic editor's separate Save and Preview controls retain their existing
behavior.

An explicit authenticated open uses the owner-scoped draft-open endpoint and a
stable operation ID. Its first-open timestamp is separate from button-offered
and delivery state; it does not rewrite the immutable artifact receipt. Loading
metadata or an image preview alone does not record an open. Both native-created
and agent-created owned drafts use the same entry and recovery rules.

## Continue with the agent or form

Both entry points read and save the same draft. An agent can continue a native
save through the existing tools; Reload saved state lets the form continue an
agent edit. Reload explicitly discards local edits. Selecting another original
or uploading a new one is disabled while local changes are unsaved.

A stale save keeps local fields visible and blocks further editing until explicit
reload. A failed draft switch restores the selector to the actual loaded draft;
it cannot display one draft while saving another.

Before a write, the controller stores an owner-bound operation ID and request
metadata in sessionStorage. Unknown outcomes block fresh writes. Check earlier
request reads the immutable original receipt, then loads current draft state,
which may already include later agent changes. Retry sends the same operation
ID and payload. It does not silently allocate a replacement operation.

Upload recovery stores only digest, size and operation ID, not image bytes.
A committed upload is recovered without a second upload. If its outcome is
unknown after reload, reselect the exact original to retry. A different source
is rejected locally. Storage failure prevents a new write; corrupt recovery data
blocks writes on that page rather than discarding the recovery record.

Late responses and late errors are fenced by owner/session epoch. Logout clears
images and preview URLs. Authentication failures use the existing account-session
flow. The page CSP allows image sources only from self and local blob URLs;
image bytes and bearer credentials are not put into conversation JSON.
