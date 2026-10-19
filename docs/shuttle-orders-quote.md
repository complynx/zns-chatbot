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

## Developer and owned-stand acceptance

Architecture and Go consultations selected this slice before implementation.
Behavior tests cover shared/per-portion charges, separate offers, absent versus
explicit empty pricing, old optional-field encoding, invalid references/modes,
fractional/duplicate/invalid quantities, arithmetic/aggregate/response bounds,
opening/closing equality, DST instants, stale catalog/event versions, owner
isolation, disablement, revocation and no extra catalog operations or payment proofs.
Focused race tests passed. The final complete race gate passed every one of the
160 enumerated integration tests in 11 fresh bounded processes, with exact coverage
recorded, plus all other explicit Go packages. No final container was OOM-killed.
Final lint reports zero issues. Initial lint failures were corrected without
weakening the configured checks.

Three independent review perspectives and a fresh final pass found no remaining
actionable issue. Review corrected unsafe agent advice to split oversized quotes:
same-offer shared charges make separate totals non-additive. Tests were expanded
for arithmetic/response bounds and stale event versions. The index stayed empty.

Native owned IAM/Loom exact approval has configured synthetic complete pricing;
an assigned-unpaid owner calculation returned 24.93 BYN. An actual message in the
authorized Telegram chat called catalog and quote through a real Loom script:
24.68 for soup + 0.20 for two containers + 0.05 for one spoon = 24.93 BYN. The reply
was visibly delivered once. An exact-approved fixture edit changed the base price;
the old version was rejected and a current quote returned 24.95. Bot restart
preserved this rejection, the fresh total and the original configuration receipt.
Payment evidence/review counts did not change. Runtime schema is 23, manifest
`mvp-23`; only the owned bot image was replaced.

The first native helper mistakenly answered a history-summary request with a tool
call. Loom correctly paused with `context_unavailable`; no order tool was dispatched.
After fixing the manual provider, the same run was explicitly resumed without a
second Telegram message. A separate QA-helper query initially selected the wrong
database and was corrected before answering the pending request. Both failures
remain recorded. No Loom product defect was established. The provider and history
summary were manual Codex fixtures, so these checks do not establish unattended
model/summarization quality or production readiness. Full order writes,
capacity, cash, recipient, cancellation/refund policy and FX remain later slices.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
