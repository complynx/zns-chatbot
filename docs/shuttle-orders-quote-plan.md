# Server-calculated owner quote plan

This preserves the design selected before implementation. The
[quote candidate and acceptance boundary](shuttle-orders-quote.md) are now
implemented and accepted on the owned stand. Architecture and Go
consultations selected a read-only calculation after the catalog foundation.
It does not choose D12 reservation, cash, refund or recipient policies.

## Explicit complete pricing

The existing catalog contains base prices only. Add an optional versioned
`complete_pricing` policy with supported version 1 and bounded supplement
definitions. Each supplement has a stable ID, en/ru label, exact minor-unit price
and charge mode `per_portion` or `per_offer`. Items can reference required
supplement IDs. Validate missing/duplicate references and unsupported modes.

Absent policy means `quote_unavailable`; do not silently make existing catalogs
complete. An explicitly empty policy is the administrator's attestation that base
prices are complete. New optional fields use `omitempty` so old normalized
arguments and successful receipts remain byte-compatible when they are absent.
Use the existing exact-approved catalog edit; no separate supplement table or
configuration endpoint is needed.

Containers charge `per_portion`, summing selected item quantities that require
that container. A given utensil charges `per_offer` once even when several selected
dishes require it. Different offers charge it separately: each offer is a dated
meal/service instance. No float, rounding or FX enters this calculation.

## Quote API

`zinusya_orders.quote` will accept explicit `event_id`, expected event/catalog
versions and bounded flat selections `{offer_id,item_id,quantity}`. Quantities
are positive bounded integers; duplicate offer/item pairs are rejected. No client
prices, totals, currency, owner ID or recipient name is accepted.

Reuse the current owner binding, module and assigned-pass checks on one transaction
connection. Assigned unpaid/overdue qualifies. Read the current catalog and DB
observation time; reject stale versions, absent complete pricing, unknown or
out-of-offer items, and any selected offer whose sales window is closed. Opening
is inclusive, closing exclusive; no wall-clock or weekday inference is added.

Return event/catalog versions, IANA timezone, observation time, currency/scale,
canonical selected item lines and server-added supplement lines grouped by
offer/supplement. Lines expose quantity, unit price and exact line amount; include
subtotals and `total_minor`. Before multiplication check the bound by division;
before addition check the remaining total bound. Aggregated supplement quantities
and the grand total must also stay within explicit limits and the catalog's
JSON-safe maximum money value.

This is an effect-read operation. It creates no quote ID, durable receipt, cart,
order, reservation, money or timer. It does not promise price validity until a
deadline: a catalog/event edit may invalidate the snapshot earlier. Any later
submission must reread and recompute current rights, versions and sales windows.
The ordinary catalog still presents base prices separately.

## Planned acceptance

Behavior tests cover shared cutlery for two dishes, containers per portion,
separate meal charges, absent versus explicitly empty pricing, invalid references,
duplicate/invalid quantities, multiplication/addition overflow and supplements,
opening/closing equality, stale versions, owner isolation and disable/revocation.
Prove no business-table writes. Native owned IAM/Loom/Telegram acceptance will
configure exact-approved synthetic complete pricing, read a quote, then change
the catalog and reject the old version. Review loops and documentation accompany
the implementation; production menu, FX and order lifecycle remain later gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
