# Saved avatar previews

Introduced at schema55 / manifest `mvp-63`, this API adds an authoritative preview of a saved
avatar draft.
It creates no artifact, operation receipt, outbox entry or agent
task.
No active Loom run is needed.

## Native and agent entry points

- Native: `GET /app/avatars/drafts/{id}/preview/{version}` with the normal
  `ZinusyaSession` authorization. The positive version is explicit in the path.
- Agent: `POST /internal/agent/avatars/preview_draft` with `draft_id` and
  `expected_version` in arguments. The normal owner authorization applies.
- The agent receives metadata and a JPEG descriptor, not image bytes in its
  conversation JSON. Its versioned download path is
  `/internal/avatar-drafts/{id}/preview/{version}`; reading it repeats owner,
  version and authorization checks.
- The public brand overlay is `GET /app/avatars/frame/{sha256}`. Only the exact
  compiled frame digest is served. It contains no user data and is immutable.

Preview renders the saved transform with the draft's pinned frame. A different
current version or unavailable frame returns409. Foreign drafts return404;
processor failure returns503. Invalid locators return400. Session expiry during
processing prevents native content delivery. Reads recheck current draft state
and authorization after the bounded isolated renderer returns; no database lock
is held during image processing.

The worker result must match source digest, frame digest, canonical transform,
output hash and bounded1024x1024 JPEG metadata. Native/internal JPEG responses
are private/no-store attachments with nosniff and sandbox headers. Old preview
links reject after a later edit; they are not durable published artifacts.

The PNG now lives in the codec-free `internal/avatar/frame` package, shared by
the bot's public asset handler and the isolated leaf. The bot still does not
import image codecs. The move preserves the frame bytes and legacy identity
JPEG exactly. Existing generation receipts and delivery are unchanged.

## Remaining work

The [native visual editor](shuttle-avatar-editor.md) now uses this API.
Draft
finalization is [in progress at the storage layer](shuttle-avatar-finalization.md);
its public API and durable Telegram delivery without Loom remain open.
A preview download does not establish
native finalization/send parity.
Production retention/discard policy remains
open.
