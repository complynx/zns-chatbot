# Native order reviewer reads

Existing owner/reviewer routes and forms,
reactive history and [durable notices](shuttle-order-notice-delivery.md) are installed.
Agent-to-native confirmation remains
unaccepted after a browser-tool input barrier; fault/outage and unattended
model acceptance remain open.
D23 money-policy questions remain open.
References
to component versions below describe the original implementation milestone.

## Routes

All routes use the existing Mini App bearer session. The server verifies IAM and
derives the actor, deployment and global-admin role; requests supply none of them.
An assigned participant pass does not grant reviewer access. Reviewers require
current orders event authority or the verified global-admin role. The registrations
module and a pass for the reviewer are not required.

- GET /app/order-reviews/events: currently discoverable orders events. Optional
  after UUID; at most 20 entries plus next cursor. This is discovery, not a grant
  to read an event later.
- GET /app/order-reviews/events/{event}: authorized event metadata for a linked
  or recovered review.
- GET /app/order-reviews/events/{event}/evidence: unresolved queue, with optional
  after UUID and at most 20 entries. Includes pending, exhausted and needs_decision.
  The cursor is UUID order, not submission priority. Reviewed claims leave this
  queue; authorized direct detail reads remain available.
- GET /app/order-reviews/events/{event}/evidence/{evidence}: current evidence,
  immutable sold order terms, payer display name/UUID and this reviewer's latest
  saved workspace, or null if none. It exposes no private registration fields or
  Telegram locator. Workspace state/current flag must be checked before later
  execution; reading it is not consent. A saved workspace may have expired or
  become superseded, cancelled or completed.
- GET /app/order-reviews/events/{event}/evidence/{evidence}/content: bounded source
  bytes for that evidence. A file UUID cannot replace the evidence UUID.
- GET /app/order-reviews/operations/{operation}: this actor's earlier review call,
  including source and stored outcome, after current session/event authorization.
  It supports agent-to-form recovery without transferring ownership or creating
  consent. Missing returns 404 and means unknown, not proof of non-execution.

JSON responses are serialized and limited to 256 KiB before delivery. Every route
uses existing Mini App request/session rules. These routes perform no business
writes: they do not prepare, cancel or confirm a decision, move capacity, confirm
money, execute refunds, or start a Loom run.

## Authorization and source lifetime

Event reads acquire the orders event lock, current actor/authority, then the
native session check in the same transaction. A logout during the event-lock
wait denies the read when it resumes. Discovery checks the actor/session before
its bounded query; event routes independently repeat current authority checks.
Response delivery also checks the active Mini App session.

A revoked payer must not prevent an authorized ambassador resolving retained
proofs. Reviewer reads therefore use evidence-scoped access, not the payer's
participant entitlement. An ordinary owner's generic file access is not broadened.
Cross-event evidence IDs and bare file IDs do not grant content access.

The file handler releases DB locks before Telegram IO. Before serving bytes it
refreshes IAM/session, repeats orders role/session/evidence association checks,
and compares the file UUID and Telegram locator with the original. Logout, role
removal or locator replacement prevents stale bytes from being disclosed.
Responses use attachment disposition, detected MIME, nosniff, private/no-store and
sandbox CSP. Losing Telegram source availability does not delete claim history.

The detail includes the same shared workspace used by the agent. Its frozen
summary and exact revision are retained; no replacement decision is invented by
this read layer. Native write adapters must use the existing shared workspace
store and exact canonical confirmation, not standalone raw review primitives.
