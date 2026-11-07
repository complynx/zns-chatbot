# Order resource configuration and frozen requirements

Manifest revision is mvp-41; schema remains40.
No dependency or migration is added.
This foundation configures requirements and stores their approved snapshot.
Order
proof intake, evidence-owned allocations, authority review and exhaustion notices
remain the next lifecycle stage; neither admission nor production readiness is
claimed here.

## One event catalog remains authoritative

Optional complete_capacity on the existing PostgreSQL catalog document contains
version=1, resources and bindings. Both arrays must be explicit; missing/null
arrays are invalid. A missing/null policy means unknown requirements. A present
policy attests completeness: unlisted offer/item pairs require no resource;
resources=[] and bindings=[] explicitly declare none. Pricing and order
submission remain available with unknown requirements, but future evidence
admission must refuse unknown snapshots pending reconciliation/reapproval.

A resource has stable id, EN/RU name and integer ceiling. Bounds are64 resources
per event lifetime,400 bindings per document,1..1,000,000 units per binding and
0..1,000,000,000 ceiling/aggregate units. Existing48KiB catalog and64KiB snapshot
limits also apply: reject oversized terms, never truncate them.

Once configured, policy removal and removal of any previously declared resource
ID are rejected during both preparation and execution. An unused resource stays
declared with no bindings; ceiling0 closes it to future admission. Labels may be
corrected, but the ID must keep identifying the same physical pool. Semantic
repurposing cannot be detected from a label edit and remains an administrator
obligation. No independent registry, occupied counter or placeholder allocation
table duplicates this configuration.

The existing global-admin orders_catalog read and set_orders_catalog
prepare/execute/status protocol is the administration API and agent tool. It
retains exact approval, five-minute expiry, event/catalog versions, current
identity checks and operation recovery. Catalog and immutable receipt commit in
one transaction under the existing operation/event/actor locks. Replaying a
successful old operation returns its original receipt without restoring its old
ceilings. No configuration operation creates orders, reservations or money.

## Bindings and sold requirements

Each binding has offer_id, optional item_id, resource_id and units. An item binding
multiplies units by selected quantity for that exact offer/item. An offer binding
without item_id contributes once when any item of that offer is selected.
Contributions add, including item and offer bindings referencing the same pool.
Duplicate triples, foreign resources/offers/items, invalid amounts and aggregate
bounds are rejected. An item must belong to the referenced offer. Resources and
bindings are normalized into stable order without changing caller data.

New order snapshots retain optional capacity={version:1,demands:[...]} with
resource ID, sold bilingual label and summed units. Mutable ceilings and available
places are not frozen or implied. New pending approvals compare the complete
snapshot; changed conditions require fresh consent. Existing completed receipts
and old orders retain their original unknown context without consulting today's
catalog. Explicit empty demands stay distinct from missing context through JSON,
PostgreSQL, receipt replay and reads after reopening the store.

Native review/history and Telegram exact consent show frozen labels/IDs/units in
RU/EN, or explicitly say requirements are unknown/none. These interfaces continue
to state that submission reserves nothing and confirms no payment. A zero ceiling
therefore does not prevent submission. Availability checks belong at admission of
payment evidence, not at catalog read or order submission.
