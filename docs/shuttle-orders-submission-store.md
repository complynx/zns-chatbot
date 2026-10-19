# Order submission store component

The store can prepare and submit an owner's saved selection, read/list retained
orders, and cancel a submitted order before its frozen sales cutoff. This is the
store component of [the agreed plan](shuttle-orders-submission-plan.md). The agent HTTP routes and manifest now have a separate
[component gate](shuttle-orders-submission-api.md). The [native transaction/context component](shuttle-miniapp-order-submission-store.md)
also has its own gate, as do native
[HTTP](shuttle-miniapp-order-submission-http.md) and
[UI](shuttle-miniapp-order-submission-ui.md). The original
[real Telegram order-submission acceptance](shuttle-orders-submission-real-qa-2026-10-17.md)
has since passed. New sold service context and
[resource requirements](shuttle-orders-capacity-foundation.md) have separate
component gates and are not yet activated on that stand.

## Transaction and recovery

Migration40 adds preparations, orders and phase-specific operation receipts.
At this store component gate, the owned main stand was at schema39 on image
`sha256:e6c429ba15abb5cc5782ddb066581cda6456a41f70be96e8e37db714aedd2170`.
No main-stand migration or activation was performed for this component.

`PrepareOwnOrderSubmission` recalculates the current nonempty saved draft under
current event/catalog/draft versions, assigned-pass entitlement and database
time. Identical pending business parameters share a proposal across transport
operation IDs. Its lifetime is five minutes or the earliest selected offer's
closing instant, whichever comes first. Preparing creates no order.

`SubmitOwnOrder` rechecks current authorization before recovering a matching
receipt. For a new effect it validates the exact proposal, recalculates the same
snapshot, and checks versions, open sales and proposal expiry. One transaction
inserts the submitted snapshot, advances the consumed draft to an empty revision,
marks the proposal consumed, and inserts the effect receipt. Receipt failure
rolls back every write. The old saved-selection receipt remains intact.

The canonical owner/event/source-draft revision prevents duplicate orders across
different transport IDs. Canonical recovery returns the immutable original
submission result after newer drafts, catalog edits or cancellation, without
clearing the newer draft. Reusing the same operation ID with different business
arguments or a different execution proposal conflicts. The prepare and submit
phases may share an operation ID; their receipts stay separate. `OrderSource`
is supplied by trusted transport code, not by user arguments.

`OwnOrder` and `ListOwnOrders` return current order state. `OwnOrderOperation`
and `OwnOrderOperationByID` return one exact immutable operation phase. A prepare
receipt is not a successful submission receipt; a missing receipt is unknown.
Every read and replay requires current owner identity, account, deployment,
module and assigned-pass access. Event-to-owner locking also serializes a draft
edit against submission. No external service call occurs while holding locks.

For a new effect, `CancelOwnOrder` requires the exact current order revision and
a submitted state, strictly before the original stored cutoff. A matching
cancellation receipt replays after the cutoff or later state changes, subject to
current authorization. Cancellation preserves the snapshot and old submission
receipt. It neither restores the cart nor changes money or capacity.
This component has no order evidence/payment intake. Its later introduction must
atomically leave the cancellable submitted state before reserving capacity.

All amounts remain exact minor-unit integers; snapshots retain currency/scale,
labels, source versions and the event IANA timezone. Stored instants use UTC.
Submission itself creates no capacity reservation or confirmed money. The later
proof intake/review stage must implement the agreed reservation, rejection and
refund-attention rules; those are not implemented here. Zero-total submission
does not assert payment or decide free capacity allocation.

## Verification and limits

The first store pass has eight new behavioral roots and eight adjacent catalog,
quote and selection roots: 16 roots/16 passing events on real PostgreSQL under
Linux with the race detector, zero failures and zero skips. It covers durable
receipt recovery through a reopened store, shared proposals, concurrent aliases,
concurrent draft edits, stale/expired proposals, changed operation bodies,
receipt-write rollback, current access on replay, cancellation guards and
refusal to roll back retained preparations. The first closed-order test constructs a
past cutoff directly in its disposable fixture. The follow-up gate adds real
three-order pagination, waitlist and foreign identity/bot/organization denial,
concurrent cancellations, concurrent cancellation versus a new order, immutable
cancellation recovery after cutoff, an exact SQL cutoff test and spring/autumn
Berlin DST snapshot persistence. This follow-up passed 22 roots/24 events on
Linux/race with real PostgreSQL; evidence is in
`.cache/shuttle-work/orders-submission-edges-v2-qa/`. The equality test supplies
an observation instant to the actual SQL guard and rolls back its probe; it is
not a public-clock/UI acceptance claim. DST fixtures keep sales open at actual
database time and place future service/cutoff instants across real transitions.

The test container was limited to 2GiB without swap and 2CPU, with Go memory limit
384MiB and two test cases in parallel. It exited zero without OOM. The exact
QA PostgreSQL, Go container and dedicated database volume were removed; all ten
main services retained their IDs, images, start times and restart counts.
Private evidence is in `.cache/shuttle-work/orders-submission-store-v1-qa/`.
The frozen manifest contains 859 source inputs and is unchanged across the run.

Host compilation passed; the scoped configured lint run reported zero issues;
`sqlc compile` passed. Initial lint findings were formatting and an if/else-chain
style warning, corrected before the frozen behavioral run. Two independent
textual review passes, correctness and code quality, returned
“No actionable findings.” Neither reviewer ran commands or tests. The Git index
remains empty; no commit, push, PR or production deployment was performed.

The follow-up review found that the concurrent cancel/submit test needed to
read the original order independently after both operations. That assertion was
added, the full 22-root gate was repeated, and a fresh textual review reported no
actionable findings. The first passing run is retained. An initial lint warning
about the parent parallel call was fixed before its frozen run. Both QA pairs
and their dedicated volumes were removed with the main services unchanged.

The native store/context and HTTP gates are documented separately in
`shuttle-miniapp-order-submission-store.md` and
`shuttle-miniapp-order-submission-http.md`. Still required before the full stage
is accepted: real native-only, agent-only and both mixed
Telegram paths, plus service-restart recovery. The form component is documented
in `shuttle-miniapp-order-submission-ui.md`. Component gates do not prove
actual Loom or Telegram execution. Full order evidence, capacity, payment and
meal fulfillment follow later.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
