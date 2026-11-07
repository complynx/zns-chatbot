# Shared order identity: service and meal

Migration54 adds explicit business identity to shared order storage. `OrderKind`
is `service` or `meal`; `OrderSource` remains the separate transport identity
(agent, Mini App or Telegram).
This is an expansion for future meal submission,
not an enabled meal financial workflow.

## Storage and service boundary

Orders, preparations and operation receipts have a non-null `order_kind`, with
`service` as the compatibility default. Existing rows gain that metadata without
rewriting JSON snapshots, operation results or request/business hashes. Current
SQL writers explicitly insert `service`; old binaries that omit the column also
remain service writers. This compatibility window assumes no meal writer is
enabled while old service-only binaries are running.

Canonical uniqueness is now `(user_id,event_id,order_kind,source_draft_version)`.
Thus independent service and meal draft version1 can coexist. Pending-proposal
lookup also distinguishes the kind. Composite foreign keys bind order to
preparation and operation receipt to its preparation/order with the same owner,
event and kind. Existing operation primary keys stay unchanged. Collision reads
inspect all kinds, so a UUID used by a meal operation cannot be reused as a
service operation; disclosure/recovery returns only service receipts.

All currently exposed order APIs remain service APIs: preparation, canonical
lookup, consumption, owner get/list and cancellation explicitly filter service.
Both service record decoders reject another kind before JSON decoding. This
prevents unsupported meal JSON from becoming a zero-valued service snapshot.
No business-kind argument or field was added to the public service result JSON.

Evidence is deliberately still service-only. Its new kind column has both a
service default and `CHECK(order_kind='service')`; a composite foreign key binds
it to the same owner's service order/event. Even a direct SQL insert cannot
attach service evidence to a meal order. Existing reviewer, capacity and notice
paths therefore keep their existing service semantics. No meal catalog/resource
policy is borrowed from generic orders, and no meal submission handler exists.

## Migration and rollback

The expansion is transactional and locks the affected tables before changing
identity constraints.
Service-only Down/Up preserves historical snapshots,
receipts, hashes and evidence.
Down refuses any retained meal order,
preparation or operation history before dropping columns/constraints.
It does
not erase or convert that history.
Run rollback only with the compatible old
application; the new binary's queries require schema54.
