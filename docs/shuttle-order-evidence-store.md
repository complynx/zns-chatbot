# Order evidence: durable store component

At that read-API checkpoint,
read/file/receipt tools used manifest mvp-42.
Submission/review mutation APIs, native
controls, notice delivery and live activation remained pending at that stage.
The subsequent
[shared review store](shuttle-order-review-workspaces.md) advances source to schema 42
without changing the read-only manifest.
The owned bot remains schema 40 on its
previously accepted image.
No production, publication, new dependency or unattended-model acceptance claim.

## Admission and current policy boundary

SubmitOwnOrderEvidence binds an explicit order/version, accepted owner file ID and
reported amount. The server derives owner, original quoted currency/scale/amount,
file provenance and Telegram timestamp; clients cannot supply those facts. It
requires current owner binding, assigned-pass entitlement, orders module and the
correct deployment. Both successful receipt recovery and new work recheck access
and file ownership. Current order state/version governs a new attempt; receipts
retain their original admission result even after authority review.

The supported admission path is exact full amount, known frozen requirements,
trusted file sent_at and explicit binding before the frozen cutoff, with every
required resource available. Admission creates pending evidence and held rows;
it does not confirm payment or fulfillment. A zero-resource explicit known-empty
snapshot needs no held rows. Unknown requirements are not silently unlimited.

Single-resource exhaustion retains the original claim with exhausted state and
refund_attention_required reason. It creates no hold or automatic refund. Mixed
shortfall, different/partial amount, unknown requirements and deadline divergence
retain file/target as needs_decision with no holds. These are persisted unresolved
claims, not a chosen financial policy or completed D23 handling. Authorities
cannot confirm them through the supported exact-full review path. They may reject
an attempt with an explicit reason; additional policy/resolution workflows remain.

One unrejected attempt per order is enforced. Pending, unresolved, exhausted and
confirmed file provenance cannot be claimed again for a pass or another order.
The existing payment-source advisory lock and combined pass/order lookup protect
both submission directions, including different events. A rejected source can be
submitted again with a new operation; its previous evidence and receipt remain.

## Transactions, authority and resource ownership

Admission uses existing operation/event/owner serialization. Evidence, applicable
allocations, business notices and immutable receipt commit together. No network
call runs inside these business transactions. An allocation belongs to one exact
evidence ID. All resource admission and catalog changes share the event lock.
Catalog preparation and execution reject any current ceiling below held plus
occupied units, including a reduction prepared before a new hold was admitted.
Ordinary order cancellation is refused once any evidence history exists, pending
explicit financial/refund cancellation semantics.

Authority review has a five-minute exact preparation binding event, evidence,
revision, decision and mandatory reason. Execution rechecks current actor,
deployment, orders module and current ambassador/administrator/global-admin
permission. The registrations module is not required. Order money validation
supports its frozen scale through6, independently of pass-payment scale4.

Confirming pending evidence changes only its held rows to occupied. Rejecting
releases only that attempt's holds. A stale attempt cannot release a replacement
attempt, and replay returns the original decision without another transition.
Confirmed capacity remains occupied. Review does not assert fulfillment or
perform a refund. Partial credit and implicit price/composition changes are absent.

Revoking payer access prevents owner reads/admission/replays but does not disable
an otherwise authorized authority's business resolution of existing evidence.
Owner identity and deployment stay checked and locked. This avoids permanent
holds caused by payer revocation. Actor revocation or loss of event authority
still blocks the decision, including receipt replay.

Review rows retain actor, role, exact amount, reason and timestamp. Current reads,
review receipts and review business notices include the decision as review data;
its reason does not overwrite the original admission reason. Notices retain only
file IDs and business metadata, never file bytes. Owner and currently known event
authorities receive distinct retained business events, with deduplication per
attempt/recipient/kind. At this store checkpoint, these rows were not delivered:
worker/outbox wiring and current-recipient authorization were still required.

Migration41 retains evidence, allocations, immutable review/preparation/operation
history and business notices. Its down path refuses retained business history;
no existing catalog/order/pass/payment history is rewritten. Successful old
catalog-operation recovery still returns the original receipt without restoring
an obsolete resource ceiling.
