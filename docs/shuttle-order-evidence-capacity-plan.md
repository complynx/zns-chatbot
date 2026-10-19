# Order evidence and event capacity: implementation plan

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Status: agreed architecture; frozen sold context and resource requirements have
passed component gates.
It is not yet activated on the live stand. See the service-context follow-up in
`shuttle-order-approval-presentation.md` and
`shuttle-orders-capacity-foundation.md`. The schema41
[order evidence store](shuttle-order-evidence-store.md) now has a component gate;
[Read-only agent evidence APIs](shuttle-order-evidence-api.md) also passed their
component gate. The schema 42 [shared review workspace store](shuttle-order-review-workspaces.md)
now passes its own gate, including exact agent/native aliases and atomic completion.
At that store checkpoint, submission/review mutation APIs, native controls, notice
delivery and live acceptance remained pending.
This is an incremental plan for the full lifecycle. Schema41 implements proof
admission and holds at store-component level; agent/native activation and
production acceptance remain pending. Earlier schema40 submitted-unpaid acceptance
is recorded separately. Root remains the sole writer; read-only architect and Go
senior consultations agreed the ownership and transaction boundaries below.

## Approved behavior and unresolved choices

Orders belong to the owner. Submitting an order reserves nothing and confirms
no money. Accepting proof for review occupies limited places; rejection releases
that attempt's places. Only event authority confirms money. If places are gone,
notify customer and ambassador that refund attention is needed; never automatically
refund, reduce the approved order or report fulfillment without capacity.

D23 in the ignored decisions file records unresolved mixed-resource exhaustion,
partial/cash and trusted proof sent before cutoff but explicitly bound after it.
No speculative policy menu or production default will decide those questions.
Implement and prove the policy-neutral path: exact full frozen quote, both trusted
sent_at and binding before the frozen cutoff, and every demanded resource available.
Single-resource exhaustion preserves proof and the original amount/composition
with durable customer/ambassador notices. Divergent inputs remain explicit
needs_decision with file/target retained; they are not silently accepted or held.
The full goal remains incomplete until these remaining policies and paths are
resolved and implemented.

## 1. Freeze sold conditions, then resource demands

First retain one service record for each selected offer: stable offer ID, EN/RU
label, absolute starts_at/ends_at. Deduplicate repeated offer references; preserve
catalog order. New preparation and submitted receipts carry this immutable context.
Both Telegram exact approval and native review/history display it in the frozen
quote's IANA event zone. Existing item/supplement IDs, quantities, exact money,
cutoff and expiry remain visible. No capacity or financial effect is added here.

Optional service fields keep old order/receipt reads compatible; absence is
historical unknown context and must never be filled from today's catalog. Old
unsubmitted preparations may conflict against a freshly computed expanded
snapshot and require a new exact preparation/consent. Existing completed and
canonical receipts replay unchanged. Respect the existing bounded snapshot size;
never silently truncate a proposed service, price or resource line.

The next foundation now retains bounded event-owned resource IDs/labels/ceilings
and explicit offer/item unit bindings in the existing PostgreSQL catalog row.
The dedicated contract documents its concrete bounds and resource lifecycle.
No second registry or migration is needed for this configuration-only foundation. Aggregate demands for repeated resource references;
freeze IDs/units with the approved order, alongside sold labels/times. Capacity
configuration must distinguish unknown/unconfigured from explicit known empty.
Historical snapshots with missing demands remain readable, but must not become
silently unlimited. Reconstruct only from proven retained revision, otherwise
require explicit reconciliation/reapproval before evidence admission.

All capacity changes use the same existing event lock; no resource lock manager
or workflow DSL. Resource IDs cannot be repurposed by catalog edits. Current
ceilings may change only with occupancy checks, never below occupied units.
No proof-intake capability is claimed for this foundation alone.

## 2. Atomic proof admission and authority review

Use dedicated order evidence/review/allocation/payment-revision/operation/notice
records, not a generalized payable ledger or pass balance. One active evidence
attempt per order; each allocation belongs to its exact evidence ID. Native and
agent routes share one caller-owned PostgreSQL transaction implementation.

After current identity/deployment/module/pass/file checks, use operation -> event
-> owner/order -> provenance lock ordering. Recover original receipt/canonical
attempt before new effects. Atomically write evidence, all applicable holds,
revision, notices and immutable receipt. Do no network calls inside that business
transaction. Native bearer generation and current owner/access remain required
on replays. Explicit order ID/version and file ID bind a proof; no sticky wait
or passive attachment automatically chooses its target or pays it.

Review prepares exact evidence/version/decision; recheck current event authority
at execution. Confirming changes that attempt's held allocations to occupied,
not free. Rejecting releases only that attempt's held allocations. A delayed
rejection of attempt A cannot release B. Receipt replay returns its original
result even when current state differs; reads expose current state separately.
Coordinate existing cancellation with intake and deny evidence-bearing ordinary
cancellation until its financial/capacity consequences are explicitly supported.

Keep payment, capacity and fulfillment states separate. Exhaustion notices report
claimed payment until a human confirms it. Persist notices transactionally and
recheck current recipient access before delivery, using durable outbox/recovery.
No proactive Loom run is added. Only file IDs belong in agent/notice payloads.

## Reuse constraints and acceptance

Schema41 uses order-specific frozen money validation through scale6; pass payment
validation/SQL remains scale4. paymentSourceAvailable now checks active/confirmed
claims across both pass and order evidence under the existing shared source lock,
protecting both intake directions. The lockPaymentEvent wrapper still requires
registrations; order evidence uses the orders-specific module choice through the
shared actor/authority helper. These store constraints have passed the schema41
component gate; native/session checks and public evidence routes remain later work.

Use focused behavior/critical-path tests with real disposable PostgreSQL: retained
service labels/times after catalog edits, old receipts and pending preparations,
last-place contention, no capacity at submission, pending not paid, reject/resubmit
and stale rejection, cross-pass/order proof reuse, revoked access/session/authority,
atomic rollback if notice/receipt fails, and same-operation recovery after lost
response/restart. Add RU/EN native and Telegram rendering cases at the responsible
layer. Freeze gate inputs; review each source stage independently before owned
activation. Then actual native/agent/mixed tests on our stand, with explicit
manual-provider/model and fixture/real-browser distinctions.

No new dependency is planned. Additive database changes must retain old data and
immutable receipts; down migration refuses retained business history. Production,
publication and developer Loom resources remain outside authorized local work.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
