# Frozen meal terms

`internal/db/meal_terms.go` now provides `FreezeMealDraft`: a pure builder of
complete descriptive terms from a retained `MealCatalogView` and saved
`MealDraft`. This is the next preparation for financial integration. It adds no
migration, HTTP route, approval, order, draft consumption or runtime dependency.
At this component gate the schema and manifest stayed53 / `mvp-60`; the later
[order identity expansion](shuttle-order-kind.md) raises source schema to54.

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

## Verification

Four new behavioral roots cover combo accounting, original names and detached
copying, deterministic encoding, both Berlin02:30 instants across DST, declined/
empty/dinner-only choices, lineage/evaluation rejection and oversized snapshots.
All32 unit roots in `internal/db` and `internal/orders` pass on Windows and
Linux/amd64 Go1.27.2 (db12, orders20). Another25 database tests skip without a DSN;
they are not PostgreSQL acceptance. No new PostgreSQL/runtime gate is claimed
for this pure builder. The db package linter reports0 issues. Independent
correctness and quality reviews and a fresh final source review have no
actionable findings.

The first lint pass found a comment prefix and a modernize fixture suggestion.
An incorrect interpretation of that suggestion caused a test-only compiler
failure on both platforms; the failed logs are preserved. The final fixture uses
Go1.27 promoted-field literals verified by the actual compiler. Final lint and
both platform runs pass. All owned Linux containers were removed without OOM;
the final source freeze is recorded. Ignored evidence is
`.cache/shuttle-work/meal-terms-v1-qa/`.

The earlier [meal runtime gate](shuttle-meal-runtime-qa-2026-10-19.md) remains its
original evidence: its image predates this builder and has not been rebuilt or
installed. The primary remains schema49 / `mvp-56`. The subsequent shared `service|meal` storage
expansion is now implemented with service-only access/recovery and evidence gates.
Meal-specific exact preparation/submission and full financial routing remain open. Financial policy questions remain
in ignored D30, and the release hold remains in force.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk


## Explicit shared deadline follow-up, 2026-10-21

After Daniel selected the legacy single-deadline policy, optional catalog
`payment_deadline` is retained in `MealTerms`. It is copied and normalized to UTC;
source mutation cannot change the frozen value. Omitted legacy deadlines remain
omitted. Service sales windows remain separate descriptive terms and do not imply
this payment-proof deadline. The builder still creates no order or payment and
performs no current-time admission. Empty-order suppression and unlimited-quantity
financial submission are agreed requirements for the next integration, not effects
of this pure builder.
