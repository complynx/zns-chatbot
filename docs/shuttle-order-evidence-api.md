# Order evidence read API

Read-API checkpoint: manifest `mvp-42`, dynamic module `zinusya_order_payments`.
This read-API gate used schema 41.
The subsequent
[shared review store](shuttle-order-review-workspaces.md) advances source to schema 42;
[mutations and human consent](shuttle-order-evidence-mutations.md) add agent writes
at manifest mvp-43.
Native controls and notice delivery
were pending at the read-API checkpoint.

## Routes and authorization

All tools use POST `/internal/agent/order-payments/<method>`. Requests use the
existing protocol version 1 envelope with operation_id and arguments. Discovery
at `discover` uses the verified bearer identity without a tool envelope. Only the
server derives actor, bot, organization and global administrator status.

- `evidence`: current owner evidence by event_id/evidence_id.
- `list`: owner history by event_id, optional order_id, after and limit.
- `operation`: the caller's original receipt by operation_id and kind submit/review.
- `fetch`: owner proof descriptor by external_id=order evidence UUID.
- `evidence_for_review`: current evidence as an authorized event reviewer.
- `queue`: unresolved pending/exhausted/needs_decision claims for an explicit event.
- `fetch_for_review`: reviewer proof descriptor, also located by evidence UUID.

Discovery exposes reviewer methods only when the current identity is a verified
organization administrator or has ambassador/administrator authority in an orders
event. This is discovery, not an access grant: every call checks the exact event
again. Reviewer access needs the orders module and no reviewer pass or registrations
module. Owner reads need current assigned-pass entitlement. A revoked payer's
historical claim can still be inspected by a current reviewer, but owner access
is denied. Owner and reviewer file purposes remain explicit, including when the
reviewer also owns the proof. Authority never broadens generic owner file routes.

An original admission receipt remains pending even after a subsequent confirmed
review.
Use evidence reads for current state. `operation` returns status not_found
without a result for a missing actor/phase receipt; this means unknown, not proof
of non-execution.
No read submits a claim, confirms money or releases capacity.

## Files and response bounds

Descriptors retain the order evidence UUID as external_id and the exact matching
fetch method. Metadata's id remains the associated file UUID. The only exposed
file is that evidence owner's associated file in the same bot/organization.
Descriptors include bounded content size, detected MIME type and SHA-256.

Bearer-authenticated downloads use GET `/internal/order-evidence/{id}/content`
for the owner and `/internal/order-evidence/{id}/review-content` for reviewers.
The server authorizes before bounded Telegram IO, refreshes authorization after
IO and compares both the file UUID and Telegram locator before releasing bytes
or a descriptor. No database lock crosses the Telegram read. Downloads use
attachment, nosniff, private/no-store and sandbox CSP headers. Source loss returns
unavailable without deleting the retained claim. No file bytes are stored in Loom
by these endpoints; consumers import them using the descriptor contract.

Each tool advertises a maximum result of 64 KiB. The server checks the full JSON
success envelope, including operation_id, and sends raw JSON so Huma cannot add
a schema link after the size check. Pages also have a default/maximum count of 20.
They shrink by bytes when necessary, with next set to the last included UUID;
follow it as after. UUID order is not submission priority. Owner history includes
reviewed claims; the reviewer queue excludes them. Exact reviewer reads still
permit historical evidence by ID. A single oversized result fails explicitly;
fields are never silently truncated.
