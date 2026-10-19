# Order evidence: durable store component

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Status: the store component gate recorded below preceded the
[read-only agent API](shuttle-order-evidence-api.md). At that read-API checkpoint,
read/file/receipt tools used manifest mvp-42. Submission/review mutation APIs, native
controls, notice delivery and live activation remained pending at that stage. The subsequent
[shared review store](shuttle-order-review-workspaces.md) advances source to schema 42
without changing the read-only manifest. The owned bot remains schema 40 on its
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

## Verification and limits

Final frozen Linux/race QA with real disposable PostgreSQL passed95 roots/189
pass events:69 integration roots/100 events,13 task roots/42 events and13 orders
roots/47 events. No failures, skipped cases or OOM;887 frozen source inputs stayed
unchanged. Scoped db/integration lint reports0 issues. The gate includes existing
pass payments, notifications and authority tests because those checks now share
the combined provenance lookup and module-specific authority helper.

New critical cases cover pending versus confirmed capacity, store reopening and
original receipt recovery, rejection/resubmission/stale rejection, concurrent last
place with durable customer/ambassador exhaustion notices, cross-pass/order proof
reuse in both directions, receipt/notice rollback and retry, authority removal,
revoked-owner resolution, unknown/partial/mixed/late claims without holds,
ceiling reduction prepared before admission and occupied-ceiling checks, orders-
only module access and currency scale6. Late-file input is a synthetic timestamp
beyond the frozen cutoff; it is not an actual clock-wait/live Telegram test.

The first gate failed ten new test roots before admission because their new file
fixture supplied non-second timestamps. Telegram attachment validation correctly
rejected them. Original output/failure classification and abort cleanup remain in
.cache/shuttle-work/order-evidence-store-v1-qa/. The fixture was corrected to
Telegram second precision; a separate v2 gate is the passing evidence. Initial
SQL qualification/nullability and an embedded-literal compile issue were also
corrected before the passing gate. No old result was rewritten into a pass.

Independent source reviews found that revoked owners could pin capacity and that
review notices lacked a self-contained human reason. Those issues were fixed and
covered before two fresh final textual reviews reported no actionable findings.
Reviewers executed no tests. Final evidence and exact cleanup receipts are in
.cache/shuttle-work/order-evidence-store-v2-qa/. Its bounded Go/PG containers and
volume were removed; all ten main service fingerprints stayed unchanged. No UI
suite was rerun for this store-only change; the preceding resource foundation has
its separate68-test UI evidence.

Next: submission/review mutation tools and native controls, persistent
shared review interactions, delivery and current-recipient checks, then actual
native-only/agent/mixed acceptance with real IAM/Loom/Telegram and recovery/fault
checks. The store gate does not prove those paths, hard-kill behavior, live
migration/rollback, notice delivery or full order lifecycle completion. D23
unanswered rules and the broader roadmap remain active.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
