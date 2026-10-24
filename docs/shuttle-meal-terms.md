# Frozen meal terms

`internal/db/meal_terms.go` now provides `FreezeMealDraft`: a pure builder of
complete descriptive terms from a retained `MealCatalogView` and saved
`MealDraft`.
This is the next preparation for financial integration.
It adds no
migration, HTTP route, approval, order, draft consumption or runtime dependency.

The caller supplies the actual retained catalog and event context. Event ID,
event version, catalog version and timezone must match the saved draft; draft
version must be positive, with an evaluation and without the cleared flag. The
context needs a bounded nonempty event name and valid named zone (`Local` is
rejected). The builder normalizes the catalog and recomputes canonical choices,
missing fields, charge lines and quote. Incomplete or mismatched evaluation fails
with `ErrMealTerms`; stored amounts are not trusted as a price calculation.

`MealTerms` retains lineage, event/day names, event zone, canonical choices,
selected item IDs/categories/full ru/en names, absolute service times and each
service's original sales window. Explicit no-lunch, empty individual selection,
empty dinner and dinner-only days remain distinguishable. Available services,
including explicitly declined ones, retain descriptive windows; these do not
choose a future order cutoff. Only `Lines` contain charges: a combo is one
charge, and its component names are not individually charged again. Individual
and dinner lines retain item IDs and final amounts. Currency, scale and total
come from recomputation. Names remain untrusted display data.

The result owns its slices/pointers and remains unchanged after source catalog
renames/removal or evaluation mutation. Repeated construction from the same
inputs produces the same JSON. Serialization is bounded to48KiB, leaving space
for a future enclosing receipt. A valid catalog/evaluation can still be rejected
when the combined descriptive snapshot exceeds that bound.

This function reads no clock, latest catalog or database and performs no
entitlement check. A future transaction must load authoritative retained records,
check current access/versions and selected sales windows at database time before
creating any proposal or order. Complete zero/empty composition is retained as
a fact; its sale treatment is not decided here. No common payment deadline,
capacity requirements or partial/cash/refund policy is inferred.

## Explicit shared deadline follow-up

After Daniel selected the legacy single-deadline policy, optional catalog
`payment_deadline` is retained in `MealTerms`. It is copied and normalized to UTC;
source mutation cannot change the frozen value. Omitted legacy deadlines remain
omitted. Service sales windows remain separate descriptive terms and do not imply
this payment-proof deadline. The builder still creates no order or payment and
performs no current-time admission. Empty-order suppression and unlimited-quantity
financial submission are agreed requirements for the next integration, not effects
of this pure builder.
