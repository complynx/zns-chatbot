# Native order reviewer reads

Current owned runtime is schema48 / `mvp-55`; the
[catalog contract image activation](shuttle-catalog-contract-install-qa-2026-10-18.md) changes catalog guidance only. Existing owner/reviewer routes and forms,
reactive history and [durable notices](shuttle-order-notice-delivery.md) are installed.
[Native proof/review](shuttle-order-proof-native-qa-2026-10-17.md) and
[a fresh native-to-agent review](shuttle-order-review-late-real-qa-2026-10-18.md)
pass their scoped real Telegram/IAM/Loom gates with a manual provider. The
[owner order/evidence gate](shuttle-orders-owner-real-qa-2026-10-18.md) now also
passes agent-only and agent/form/agent owner flows, leaving payment pending.
The [reviewer dialogue and cancellation QA](shuttle-order-reviewer-dialogue-qa-2026-10-18.md) passes pure-agent confirmation and
free-text cancellation of a prepared review. Agent-to-native confirmation remains
unaccepted after a browser-tool input barrier; fault/outage and unattended
model acceptance remain open. D23 money-policy questions remain open. References
to component versions below describe the original implementation milestone.

Source schema 42 / manifest mvp-43. This component adds session-bound reviewer
reads alongside the [owner evidence API](shuttle-miniapp-order-evidence-http.md)
and [owner form](shuttle-miniapp-order-proof-ui.md). It adds a sqlc query, not a
migration. [Native save/cancel/confirm routes](shuttle-miniapp-order-review-writes.md)
and reviewer UI are installed on the current owned runtime; scoped acceptance is
linked above.

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

## Verification and limits

The isolated Linux/PostgreSQL race gate passed 79 roots / 147 pass events:
66 integration roots / 101 events and 13 API roots / 46 events. No failures,
skips or OOM occurred. All 919 frozen inputs matched before/after. Scoped db/API/
integration lint passed with zero issues. Two initial independent reviews and a
fresh final source review found no actionable defects.

Four new behavioral roots cover orders-only authority, discovery and shared
agent workspace/operation reads, owner denial, revoked-payer detail/content,
evidence/file/event scoping, safe response headers, logout/role/locator changes
during real handler source IO, and logout while waiting on the event lock.
These use real Go HTTP/store code with test IAM/Telegram peers and disposable
PostgreSQL; they are not real Zitadel/Loom/Telegram manual acceptance.

Ignored proof artifacts are in .cache/shuttle-work/native-order-review-read-v2-qa/.
The accepted gate's exact containers and volume were removed; the ten owned main
services were unchanged. The first gate remains separately in the v1 directory:
its fixture produced exhausted evidence but attempted to prepare a confirmed
review, which the domain correctly rejected before the new HTTP reads. The test
now prepares a valid rejection draft; it does not execute that decision. The
four initial shadow-variable lint findings also remain recorded. No domain rule
was weakened to make the fixture pass.

At that original read-component milestone, reviewer UI, reactive context,
notices, activation and real-service/fault QA were pending. Current installed
state and scoped native/fresh mixed acceptance are linked at the top of this page.
The timely-proof/late-review gate does not decide the remaining partial-payment
or mixed-capacity policies. These read endpoints imply no production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
