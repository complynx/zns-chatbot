# Order evidence mutations and human consent

## Agent API

All methods use POST /internal/agent/order-payments/<method>. The verified bearer
identity determines the actor, bot, organization and roles. Mutation envelopes
use protocol_version=1, a nonzero operation_id UUID, the identical idempotency_key
and arguments. Status calls use protocol_version and that same operation_id.
Missing status means unknown, never proof that the operation did not execute.

Owner methods:

- submit: explicit event_id, order_id, expected_order_version, accepted file_id
  and amount_minor. The stored Telegram file supplies the arrival time. This binds
  reported evidence; it does not confirm payment, fulfill an order or refund money.
- submit/status: the original immutable admission result. Read evidence for the
  current state after a reviewer decides.

Reviewer methods require current orders-event authority on every call:

- review_workspace: current actor-owned intent by event_id/evidence_id;
  workspace:null means no saved review. No registrations module/pass is required
  for the reviewer.
- review_operation: original source receipt by operation_id; receipt:null is unknown.
- save_review and save_review/status: full review plus expected_revision (0 only
  when absent), optional language auto/en/ru. Save creates intent, not a decision.
- cancel_review and cancel_review/status: workspace_id and revision. This stops
  pending consent; it does not reject evidence, release its holds or cancel/refund
  the order.
- review_commit/prepare: operation_id and unchanged workspace_id/revision/full
  review. Returns prepared, exact arguments, preconditions.proposal_id, description
  and expires_at. Retrying does not renew expiry.
- review_commit: repeats those arguments and confirmation.mode=exact_parameters
  with confirmation.preconditions.proposal_id. Changes require a new saved revision
  and fresh consent. Its success and review_commit/status return the same result:
  canonical_operation_id and evidence.

Without a result, a valid prepared commit is pending. Cancelled, superseded,
expired, missing-summary or already-decided intent returns terminal cancelled
status with an error code. A completed canonical decision takes precedence over
freshness: if another approved interface completed it, status recovers the same
result read-only. It does not create another decision or an outer write receipt.
Current authorization is still required. Save/cancel succeeded means intent only.

No remote standalone review writer is exposed. Existing admission rules remain:
exact timely full-price claims can hold available resources and be confirmed;
unsupported partial/mixed/late cases remain needs_decision, and exhausted cases
require refund attention. There is no automatic refund execution.

## Complete immutable presentation

Each saved revision stores a complete literal EN/RU summary: frozen event name,
order UUID, current payer identity at preparation, reported/frozen/confirmed amounts,
Telegram arrival and order-binding times, evidence deadline, approval expiry,
mandatory reason and proof UUID. It includes every frozen item, required charge,
quantity/unit/line price, sold service interval and held-resource effect. Times use
the sold event's IANA timezone with RFC3339 offsets. Rejection shows zero confirmed
amount and states that it only releases this proof's holds, without refund/cancel.
Untrusted labels and reasons remain literal inside a dynamically sized code fence.

Explicit language selects the user's current message language. Auto/omitted uses
reviewer bot preference, Telegram language and default: ru/be/uk map to ru; other
languages map to en. A later language change does not rewrite an old revision.
Old summary-less shared intent cannot execute anew; completed receipt recovery is
preserved. Save a fresh revision to obtain current consent.

The task adapter reads the stored agent preparation with current IAM authority,
checks the exact recipient/arguments/expiry and delivers the whole stored summary
instead of abbreviated Loom text. Existing rendering/outbox chunking puts the
reply keyboard only on the last chunk. Replies remain ordinary contextual agent
input, without visible tags.

Tool success envelopes are bounded to 64 KiB after JSON encoding and use raw JSON.
Persisted review-call documents are limited to 56 KiB, reserving envelope/result
space. Oversized input fails inside the transaction; summaries are not truncated.
