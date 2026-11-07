# Native pair invitation API foundation

The source now exposes owner-scoped Mini App endpoints for the existing pair
invitation lifecycle.
Native and agent entry use the same business transaction,
versions, holds, notifications and immutable operation receipts.

## Native endpoints

All routes require the existing Mini App origin/session rules and fresh server-side
IAM authentication. Browser session credentials are not agent/admin bearer tokens.
The authenticated owner, bot, organization and subject come from the session.
There is no public partner directory or arbitrary profile lookup.

- GET `/app/pairs/events` uses existing registration-event discovery and its
  `after` cursor. Discovery does not establish invitation eligibility.
- GET `/app/pairs/events/{event}/context` reads the owner's current waitlisted
  registration version, role and event-local invitation deadline. The form must
  show the allocation hold and require an explicit invitation request.
- GET `/app/pairs/invitations` lists invitations with optional
  `direction=incoming|outgoing`, `event_id`, `state`, `after` and `limit`. Default direction is incoming and state
  pending; `state=all` includes retained history. The bounded limit is1–20.
- POST `/app/pairs/invite` accepts `operation_id` and `arguments` containing
  `event_id`, `target_user_id` (the existing public partner code) and
  `expected_version` of the inviter registration.
- POST `/app/pairs/accept`, `/decline` and `/revoke` accept `operation_id` and
  `arguments` containing `event_id`, `invitation_id` and its `expected_version`.
  Accept additionally requires the explicit complementary `role`. Decline/revoke
  do not accept a role.
- GET `/app/pairs/operations/{operation}?kind=invite|accept|decline|revoke` recovers
  that owner's exact historical receipt. URL-encode the opaque key. Keys are
  nonempty strings of at most128 bytes, preserving existing agent keys; callers
  may generate UUID strings but the server does not require UUID syntax.

Successful writes return an `invitation` object and an optional `pair` object.
HTTP200 does not by itself mean acceptance: deciding a still-pending invitation
after its deadline returns `invitation.state="expired"` with no pair. Display the
returned terminal state and refresh current invitations. Never show a pair as
created unless the result actually contains it.

Write bodies reject unknown fields and trailing JSON. Malformed arguments return
400, inaccessible/missing records404, wrong participant403, and changed invitation
or registration prerequisites409. Missing/expired session credentials follow the
existing401/reopen flow. Temporary failures return503 with Retry-After. A reply
lost after commit must be reconciled using the original key, kind and arguments;
reauthentication does not authorize a replacement operation.

## Shared safety and recovery

Native writes take the existing operation → event → actor SHARE locks, then lock
and validate the session inside that same transaction before any receipt replay.
Owner, current subject/organization, key generation, expiry and logout/revocation
are checked. The owner lock stays SHARE: raising it to UPDATE would deadlock
concurrent requests for different events. Logout and identity mutation serialize
against the held owner/session locks.

Exact native/agent retries share the original canonical result. A key reused for
another kind or body conflicts. An invitation receipt remains the original pending
result after acceptance; it is not the current invitation state. Reads recheck the
session before exposing a response. All eligibility, target consent, event closing,
registration prerequisites and hold release stay in the existing pair service.
Acceptance joins the pair waiting list; it neither assigns nor pays for a pass.

The next user-triggered agent request now includes up to five committed invitation
operation facts across both entry points. Facts are scoped by bot, organization,
authenticated subject, local owner and task time. They contain operation/kind,
event/invitation IDs, receipt state and completion time, without partner names,
credentials or money. They are explicitly untrusted historical facts, not new
instructions, consent or necessarily current state. Form writes do not start
an agent run. The agent must refresh current state before acting.
