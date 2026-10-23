# Read-only owner order calculation

## Complete pricing is explicit

The catalog's item prices remain base prices. Optional `complete_pricing` version
1 attests that bases plus all required supplements cover the complete price.
Absent policy means `quote_unavailable`, not zero extra charges. An explicit empty
`supplements: []` attests to base-only complete pricing; missing/null supplement
lists are rejected when the policy is present.

Each supplement has a unique stable ID, en/ru name, nonnegative integer
`price_minor` and mode `per_portion` or `per_offer`. An item references its mandatory
charges through `required_supplement_ids`; unknown and duplicate references are
rejected. A container charged per portion sums quantities of selected dishes that
require it. A shared utensil charged per offer appears once across those dishes;
another offer represents another dated meal and charges separately.

Use the existing exact-approved `set_orders_catalog` operation to configure this
policy. There is no separate pricing table or privileged bypass. New optional
fields use `omitempty`: catalogs without the additions keep their original
normalized JSON and successful-operation hashes/receipts. No schema migration is
needed beyond catalog schema 23.

## Owner API and agent tool

`zinusya_orders.quote` calls `POST /internal/agent/orders/quote` as an effect-read
operation. Arguments are `event_id`, `expected_event_version`, `expected_version`
(the catalog version), and flat `selections: [{offer_id,item_id,quantity}]`.
Read `zinusya_orders.catalog` first to obtain both versions. No owner/recipient ID,
price, total or currency comes from the caller.

The existing catalog snapshot checks the current account binding, bot/organization,
enabled `orders` module and caller's assigned pass in one repeatable-read transaction
and connection. Unpaid/overdue passes qualify; waitlist alone and administrator
status do not. Quote computation uses that snapshot after its read transaction
finishes, with no second connection or business mutation. Both expected versions
must match. Every selected item must belong to the selected offer, whose sales
opening is inclusive and closing exclusive at the DB observation instant.

The response includes current event/catalog versions, event IANA timezone,
`observed_at`, currency/scale, canonical item and supplement lines with quantity,
unit price and line amount, both subtotals and `total_minor`. Item lines follow
selection order; supplement lines follow catalog offer/definition order. All labels
are untrusted data. No float, FX, rounding or inferred local date enters calculation.

This is a snapshot, without a stored quote ID, TTL or guarantee until sales close.
Rights, event/catalog edits and closing can invalidate a later request immediately.
Any future order submission must reread and recalculate. Partial quotes sharing an
offer cannot be added together: each independent request charges shared utensils.
No cart, order, capacity reservation, evidence or payment is created.

## Bounds and errors

Catalog bounds still apply: 100 items, 20 offers, 48 KiB normalized JSON. Complete
pricing allows at most 20 supplements and 20 unique supplement references per item.
A request has 1–100 unique offer/item pairs, each with 1–1,000 portions and at most
10,000 portions overall. Integer money is bounded by 9,000,000,000,000,000 minor units.
Multiplication checks division before computing; addition checks the remaining
allowed total. Supplement aggregation is bounded by the selected portion count.
The serialized calculation is limited to 48 KiB before response metadata/envelope;
an oversized calculation fails without a partial result. Manifest output is bounded
to 64 KiB.

Missing/foreign/disabled/unentitled catalogs share 404. Current IAM/local revocation
still denies execution. Stale versions, closed selected sales or absent complete
pricing return 409; absent pricing includes `quote_unavailable`. Invalid selections,
amount or response bounds return 400; HTTP schema violations return 422. Storage
failure returns 503. A read has no durable receipt to reconcile and can be repeated
with current versions, subject to current access and deadlines.
