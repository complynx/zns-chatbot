# Owner order submission and cancellation plan

## Outcome and existing seams

The next orders slice turns a saved selection into a durable owner-only submitted
order, shows its exact contents/price, and permits cancellation before any proof
or payment attempt. Users can do this through the native Mini App without Loom,
through the agent, or switch between those paths. The current catalog, exact
quote calculator, assigned-pass entitlement and versioned selection remain the
business inputs. Reuse the existing store/API/operation-receipt patterns; add no
library, executable, queue or generic workflow framework.

This is one stage of the full orders/meal lifecycle, not its completion. It
creates no capacity hold or confirmed payment. The agreed D12 transition is:
accept a proof into pending review and reserve capacity; the ambassador's later
confirmation records money, and rejection releases the reservation. Daniel chose
this in response to the options of order creation, proof receipt, or ambassador
verification: «Место занимается при принятии подтверждения оплаты, и освобождается
если отказ». This matches the retained Python intake reservation, without its
paid shortcut. If capacity is gone at proof intake, tell the owner and alert the
ambassador that a refund needs attention. See the agreed rules in
[infrastructure priorities](shuttle-infrastructure-priorities.md). Later proof/review work must implement these rules before full orders
acceptance. Cash, partial payments, refunds and proof-bearing changes are separate
open policies; do not infer them from pass payments or legacy paid flags.

## State and immutable snapshots

A submitted order has a stable ID, owner/event, source draft revision, order
revision, submitted/cancelled state and timestamps. Store its exact selections,
quoted item/supplement labels and minor-unit totals, currency/scale, timezone,
source event/catalog versions, the frozen order sales cutoff (earliest selected
offer closing instant) and server observation instant. Historical orders
retain these snapshots after catalog edits or deadline closure. No client price,
recipient, owner or paid flag is accepted. Existing orders may coexist for an
owner/event; the Python flow allows multiple orders.

Submission binds the current nonempty saved draft and current event/catalog
versions to a short-lived exact-parameter preparation. Recalculate with the
existing complete-pricing calculator in the write transaction; reject changed
versions, quantities, price, closed offer windows or expired preparation. Use
server time and the event's IANA timezone. Do not introduce Python's Grodno date,
conversion or currency constants as defaults. A zero-total quote remains a
submitted order without a payment assertion; free/limited-capacity fulfillment
requires an explicit later policy, not fabricated payment evidence.

The submission transaction inserts the order/snapshot, immutable operation
receipt and an empty next draft revision together. It clears only the exact
consumed revision. A concurrent draft save either wins before submission and
makes its preparation stale, or sees the advanced draft version and must reload;
never discard later quantities. The old selection receipt remains unchanged.

## Deduplication and authorization

Use the source owner/event/draft revision as the canonical submission identity,
with a database uniqueness constraint. Different transport operation IDs cannot
submit that same draft revision twice. The canonical business-argument hash
excludes transport IDs. Identical retried submission parameters recover the same
order; changed parameters conflict. A new identical order requires an explicit
new draft save/revision. Original operation receipts remain immutable even after
cancellation or newer orders; current status is a separate owner read.

Follow the existing event-to-owner lock order. Check current bot/organization,
identity binding, account state, enabled orders module and assigned pass in the
transaction before any read, receipt replay or effect. Unpaid/overdue assigned
passes qualify; a waitlist or global administrator identity alone does not.
After this authorization, recover an existing matching operation/canonical
submission before checking freshness for a new effect. Expired preparations,
consumed/advanced drafts, later catalog edits or cancellation cannot turn a
completed identical submission into another write or break receipt recovery.
A new effect still needs every current version, deadline and preparation check.
Missing operation status is unknown, never proof of non-execution. Recovery uses
the original key/body and current authorization. Do not hold a database lock
while calling IAM, Telegram or Loom.

Cancellation takes exact order/event ID and expected order revision. Only a
submitted order without a payment attempt or proof, strictly before its frozen
sales cutoff, may transition to cancelled;
retain its snapshot and immutable cancellation receipt. Future proof states must
fail that guard. Preserve Python's closed-order cancellation restriction: a later
catalog change does not reopen this order. A repeated identical cancellation
recovers its original result even after its cutoff;
concurrent cancellation/submission never cancels a different order. Changing a
submitted order is not implicit repricing: cancel an eligible order and submit a
new draft. Proof-bearing changes remain outside this stage.

## API, native UI and context

Expose owner list/detail, submit prepare/execute, cancellation and operation
status through the same business service. Submission requires exact approval in
the agent and an explicit confirmation of the prepared price in the Mini App.
Plain user text remains agent-interpreted; add no text tags or intent parser.
Cancellation before proof has no money or capacity effect, but still requires the
explicit owner request, exact IDs and version checks through either interface.

The manifest must declare each effectful method's confirmation/preparation mode,
idempotency scope, durable request binding, deduplication window and status path.
Document canonical source-draft deduplication as well as transport-key replay:
changing a transport ID cannot create another order from that consumed revision.
Loom status probes send only protocol_version and operation_id. Resolve that
owner-scoped receipt's event privately, recheck current event/owner access before
disclosure, and expose read-only recovery for the original result. Do not require
extra arguments that the runtime does not send or infer absence as non-execution.

Extend /orders and the existing ru/en Mini App with review/submit, submitted
orders and eligible cancellation. Show that submission has not reserved capacity
or confirmed money. All displayed times use the order/event zone. Session writes
retain current Mini App authentication, expiry/revocation and CSRF protections.
Native and agent calls share the preparation and canonical result. A form change
is context for the next explicit user message; it does not wake the agent.
Record the trusted source channel on operation receipts and use a bounded,
authorization-filtered recent-action projection rather than a second workflow log.

Use an additive migration for order snapshots, preparations and operation
receipts. Guard down migration against any retained order/proposal/history.
Bound payloads and pages using the existing quote limits. Keep native revision
and durable operation recovery working across bot/Loom/IAM outages.

## Acceptance and delivery sequence

These are component gates, not a finished user feature.

Then expose API/agent and native form/context together. Run focused behavioral
happy/critical paths and independent correctness/quality/security review loops.
Use owned-stand real IAM/Loom and actual Telegram for native-only with Loom down,
agent-only, native-to-agent and agent-to-native continuation of one prepared
submission, plus cancellation and restart/lost-reply recovery. Preserve failures.
Only then mark this submitted/unpaid stage accepted. Full proof/capacity/review,
meal lifecycle, real-model and production acceptance remain separate required work.
