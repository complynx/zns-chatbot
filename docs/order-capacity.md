# Durable order capacity reservations

Migration047 expands the order schema with `core.order_capacity_slots`. Each
event/service/zero-based seat is permanent. Nullable reservation fields retain
the order identity, payment attempt token, attempt creation time and reservation
time. A partial unique index permits only one seat per event/service/reservation.
Reservation IDs deliberately have no order foreign key: the Python source can
contain a bare in-flight claim before an order exists.

## Runtime transition

Order commands already hold an exclusive event row lock. Slot creation,
availability, claim/adoption/displacement, cancellation, order versions, payment
state, capacity notices and idempotency receipts now share that transaction.
There are no separate leases or new workers. A receipt failure rolls everything
back; exact operation replay does not touch slot timestamps or state.

Only proof/paid orders reserve seats. Cash requests and unpaid choices remain
unreserved and lose a limited extra when the service is full. Historical prices
are preserved when an extra is removed, and existing history/notification paths
record the change. A payment keeps its existing seat and reservation time when
its attempt token is unchanged. A newer attempt for the same order can adopt
that seat only when its attempt time is strictly later (or the old claim has no
attempt time). An older attempt cannot clear a newer token's reservation.

A new claim takes the lowest free seat first. Only when none is free can an
earlier payment displace a strictly later `(attempt time, order ID)` priority,
choosing the latest existing claim. A claim with no attempt time is not a
displacement candidate. Paid reconciliation processes payment time, falling
back to order creation time, then order ID. These are the active Python
`reserve_service_seat` and `reconcile_capacity` semantics, serialized by the
existing PostgreSQL event lock instead of Mongo compare-and-swap retries.

Explicit unpaid/cash deletion releases that order's selected-service claims
without requiring a payment token, matching Python deletion. Payment cancellation
still matches its attempt token. Orphan cleanup releases tokened claims for
missing/deleted orders; unrelated bare in-flight claims remain even if old.
The source has no reservation timeout: `reserved_at` is
provenance, not an expiry. The Python notification-claim timeout is unrelated.
No new seat-expiry policy is introduced.

Catalog capacity increases add free seats. A reduction that would orphan an
occupied seat fails with `capacity_configuration_conflict`; it does not delete
or move that allocation. Empty seats beyond a reduced capacity remain stored
but are unavailable. Business catalog changes require explicit reconciliation.

## Existing databases and rollback

Migration047 assigns each existing paid/proof choice a slot using the preceding
Go priority order. It does not modify orders, totals, proof references, states or
versions. If existing paid allocations exceed the catalog capacity, migration
fails and PostgreSQL rolls back the schema expansion. Resolve the inconsistency
explicitly; the migration never chooses records to discard. New empty events
get their slots lazily on their first successful order command.

Normal attempts preserve their token and effective time. Preexisting target
records without a token use `legacy-proof:<proof ID>`, or `legacy-order:<order ID>`
when they have no proof, as a target compatibility identity. This fallback is
not a converter for Python `legacy-validation` identity. Source import must
provide the exact resolved source attempt token/time and a target order ID
whose priority ordering preserves source ties, or remain blocked.

Rollback is stop the new writer and retain the added table. The preceding
binary can still read the unchanged order schema, but it does not maintain
reservations. Running it after claims have been imported would lose behavioral
parity. Before returning to the new writer after any old-binary writes, audit
and reconcile both representations while writers are stopped. No automatic
drop/down migration or mixed-version write window is supported. Keep the
single-writer deployment policy.

## Verification and remaining gates

Synthetic real-PostgreSQL tests cover stable seat adoption, simultaneous replay,
token-bound release, bare claims, deleted payment cleanup, priority displacement,
receipt-failure rollback, prior-schema preservation and atomic refusal of
overbooked prior data. Existing orders integration tests pass before and after
the change. These checks do not constitute independent Code/Functional QA.

Full import still needs effective catalog configuration, same-bot owner and
proof identity mapping, durable source references, apply and no-overwrite
reconciliation. The importer must run with runtime writers stopped, insert the
source slot allocation rather than regenerate it, and reject unresolved/null
attempt semantics. The bot still needs current-event selection instead of
`sandbox-festival`. This prerequisite does not authorize production import or
prove complete legacy orders/food parity.
