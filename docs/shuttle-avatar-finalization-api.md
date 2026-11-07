# Saved-draft avatar finalization API

The [native Generate/send UI](shuttle-avatar-send-ui.md) is implemented and has
isolated browser proof.

## Finalize and reconcile

An explicit user request supplies a nonzero operation UUID, saved draft ID and
positive expected version. Finalization creates an immutable JPEG and queues its
document delivery. It never changes the Telegram profile photo. Success proves
persistence and admission to delivery, not Telegram receipt.

Both transports check the exact committed operation before reading or rendering
an original. A matching replay skips the renderer and passes through the existing
commit method with an empty render result. The transaction still checks current
owner authorization and, for native calls, bearer/key-generation/session state
before returning the original receipt. Later draft edits or a renderer outage do
not invalidate an exact historical replay. Different arguments with the same
operation conflict.

Fresh requests render the saved original and pinned frame outside database locks,
using the existing bounded processor. API identity is checked again after render;
commit serializes current ownership, saved version, provenance, quota and session.
A concurrent exact request may commit while rendering is in progress. On a render
failure the API checks the exact operation again: if it now has a receipt, commit
replays it instead of reporting a terminal stale-draft rejection. Failure to
reconcile is propagated; it does not authorize another generation.

Generation receipts contain draft/version, source/frame hashes, transform,
processor version, JPEG hash/size/dimensions, artifact/delivery IDs and completion
time. Their shape has no fake legacy task/source-file IDs. Existing legacy avatar
routes and receipt schemas retain their task-based contract.

## Agent tools

The static zinusya_avatars module adds these methods:

- finalize_draft accepts arguments.draft_id and expected_version, with matching
  operation_id/idempotency_key in the existing protocol-version1 write envelope.
- finalization_operation returns the immutable original receipt. Its status path
  accepts operation_id alone; explicit lookup also accepts arguments.operation_id.
- finalized reads the receipt plus current delivery_state/delivery_version by
  artifact_id. No recipient, attempt UUID or transport receipt is returned.
- fetch_finalized returns an owner-authorized file descriptor for the immutable
  JPEG. Content stays in Zinusya; no image bytes are embedded in conversation JSON.
- finalized_list returns retained saved-draft artifacts newest first, bounded by
  the existing shared20-artifact quota. It recovers locators after local browser
  state is lost and does not include legacy task-based artifacts.

Every method requires current owner identity. The standard short-lived positive
IAM cache policy applies, with local account checks and transactional owner checks
on mutation. A module declaration or an artifact ID is not authorization.
Finalization itself has no extra approval mode: the explicit user request is its
intent. Uploaded images, draft edits and historical actions are not new consent.

A missing operation returns not_found; it means no known committed receipt for
that owner/operation. Reuse and reconcile the same ID/payload after an uncertain
HTTP result. It does not mean a document was not delivered and does not permit
regenerating or retrying a delivery under a new operation. Unknown delivery uses
[explicit administrative resolution](shuttle-avatar-delivery-resolution.md).

## Native HTTP

- POST /app/avatars/drafts/{id}/finalize accepts operation_id and expected_version.
- GET /app/avatars/finalizations/operations/{operation} reads its immutable receipt.
- GET /app/avatars/finalized lists the owner's retained artifacts and current state.
- GET /app/avatars/finalized/{id} reads one receipt and current delivery state/version.
- GET /app/avatars/finalized/{id}/content downloads that owner's immutable JPEG.

Native mutation uses the existing strict JSON, origin and session guards; no
caller-selected owner, recipient, run or event is accepted. Reads recheck the
current session before returning metadata or bytes. Downloads use JPEG attachment,
private/no-store, nosniff and sandbox CSP headers. Internal descriptor download is
GET /internal/finalized-avatars/{id}/content with the current owner's bearer token.

Native operation absence is HTTP404, stale draft or changed operation is409,
invalid input is400, and an expired session is401. The agent transport retains
its succeeded/rejected envelope; processing or storage unavailability is503.
The existing20-artifact quota is shared with legacy generation. Original-image
retention/discard policy remains a separate pre-production decision.

## Reactive context and acceptance

The existing native action stream combines completed draft edits and native
finalizations, then applies the task-created cutoff, deterministic ordering and
limit5.
Finalization entries include original draft version and artifact/delivery
locators.
Agent-origin admission remains agent-origin after a native replay.
The next user
request can inspect current draft/delivery state and continue from either path.
History is untrusted metadata, not instructions or consent to generate/resend.

Focused proof uses isolated real PostgreSQL/River and mock IAM/Telegram/Loom peers.
It covers both HTTP directions, immutable receipt replay after edits and renderer
outage, current owner/session guards, concurrent identical requests, edits/session
expiry during rendering, metadata/download isolation, native action history and
actual document delivery with Loom unavailable.
Review identified a concurrent
commit/edit/render-failure race; a controlled test first reproduced the incorrect
409 response before the reconciliation fix.
Failed fixture attempts remain in
separate evidence directories.

The native Generate/send UI now includes owner-bound pending-operation recovery.
Actual Telegram WebView, unattended model and production readiness remain open.
