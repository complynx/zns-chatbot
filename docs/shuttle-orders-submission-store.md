# Order submission store component

The store can prepare and submit an owner's saved selection, read/list retained
orders, and cancel a submitted order before its frozen sales cutoff.
This is the
store component of [the agreed plan](shuttle-orders-submission-plan.md).
The [native transaction/context component](shuttle-miniapp-order-submission-store.md)
also has its own gate, as do native
[HTTP](shuttle-miniapp-order-submission-http.md) and
[UI](shuttle-miniapp-order-submission-ui.md).
New sold service context and
[resource requirements](shuttle-orders-capacity-foundation.md) have separate
component gates and are not yet activated on that stand.

## Transaction and recovery

Migration40 adds preparations, orders and phase-specific operation receipts.
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

Stored instants use UTC.
Submission itself creates no capacity reservation or confirmed money.
Zero-total submission
does not assert payment or decide free capacity allocation.
