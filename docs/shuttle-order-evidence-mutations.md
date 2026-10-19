# Order evidence mutations and human consent

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Source: schema 42, manifest mvp-43, module zinusya_order_payments. This component
adds agent HTTP mutations and complete stored consent to the shared review store.
It does not upgrade the owned main stand (schema 40 / manifest mvp-40).
[Native owner HTTP](shuttle-miniapp-order-evidence-http.md) is verified separately.
Owner UI, reviewer HTTP/UI, reactive action context and business-notice delivery
were outside this mutation checkpoint; their subsequent source is linked above.
Real order-evidence acceptance remains pending.

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

## Verification and limits

The isolated PostgreSQL/Linux/race gate at the ignored local path
.cache/shuttle-work/order-evidence-mutations-v3-qa/ passed 53 top-level tests /
110 pass events: 40 integration roots / 64 events and 13 API roots / 46 events.
All 909 frozen inputs matched before/after. There were no failures, skips
or OOM. Scoped db/API/task/integration lint passed with zero issues.

New behavioral coverage checks owner submission/retry/original status after human
review, agent save to native commit to read-only agent status, native save to agent
HTTP preparation/commit/retry, changed-argument rejection, cancellation status,
and complete forty-item Russian consent through task/outbox/mock Telegram before
the final keyboard. Existing authorization, concurrency, rollback and store-restart
scenarios also passed. Independent correctness/quality reviews and fresh final
source reviews returned no actionable findings. These are component tests with
mock IAM/Loom/Telegram, not real Telegram/native form or unattended-model acceptance.

The earlier v2 QA remains failed: its new delivery test awaited completion of a
storage-only paymentFile fixture lacking normal Telegram intent. Delivery was not
reached. The replacement uses Service.Accept with a normal Telegram document and
extracts its persisted file ID. The original failure, source freeze and diagnosis
are retained separately. No product behavior was weakened to pass it.

Temporary Go/PostgreSQL containers and their labeled volumes are removed after
the gates; the ten main services are unchanged. Next: native owner proof targeting,
reviewer HTTP/forms over the same shared revisions, reactive action context,
durable authorized notice delivery, then owned-stand migration and real
native/agent/mixed/restart/fault acceptance. Publication and production remain held.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
