# Native avatar editor

The embedded Mini App now has an avatar draft editor. The editor was introduced at schema55 / mvp63; the
[shared finalization APIs](shuttle-avatar-finalization-api.md) extend current source
to schema57 / mvp65. The installed primary is schema49 / mvp56.
No new dependency, configuration or schema is required by this UI stage.
These changes are not installed on the primary or production.

## Use

Open the existing Mini App account page and find Avatar. Select a retained draft,
or explicitly upload a new JPEG/PNG original, at most20 MiB. Existing evaluation
admission limits remain five originals and50 MiB per owner; no automatic deletion
policy is inferred.

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

## Acceptance boundary

RU/EN labels, behavior tests, API asset serving and isolated real-browser canvas
checks are recorded in [editor QA](shuttle-avatar-editor-qa-2026-10-19.md).
The browser backend is a fixture; it is not full runtime or Telegram acceptance.

The [finalization storage foundation](shuttle-avatar-finalization.md) is in
progress; shared [finalization APIs](shuttle-avatar-finalization-api.md) and task-free
delivery and the [Generate/send UI](shuttle-avatar-send-ui.md) are implemented.
[Owned runtime acceptance](shuttle-avatar-runtime-qa-2026-10-19.md) now passes all four paths
with real IAM/Loom and mock Telegram. Actual Telegram WebView and unattended model
acceptance remain open. Legacy
agent generation/send still has its existing task requirements. Completed editor
parity has native-only finalization with Loom stopped, agent-only completion,
and both owned-runtime handoffs, including a normal bot restart. This uses a
Codex-fed deterministic provider, not an unattended model. Production
retention/discard policy remains pending.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
