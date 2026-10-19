# Event-bound orders foundation plan

This preserves the design checkpoint before the catalog foundation. That bounded
[catalog/read slice is now implemented and accepted](shuttle-orders-catalog.md);
the complete order lifecycle remains future work. Architecture and
Go consultations reviewed the legacy flow before selecting the slice.
Allocator ordering, pair refund policy and production export scope do not block
the catalog/read foundation below.

## Legacy constraints to retain deliberately

`zns-chatbot/plugins/orders.py` combines meals, extras and capacity-limited services.
It uses separate exclusive food/extras deadlines and a historical local `now_msk`
helper. Food presence selects the whole order's cutoff; an old food order cannot
be edited into an extras-only order after that cutoff. Current constants are
specific to Grodno/Europe-Minsk and must not become global rewrite defaults.

The server recalculates prices. Mandatory containers apply per portion; cutlery
applies once per meal. A menu's base price therefore is not a final payable quote.
Legacy RUB/BYN conversion and rounding are distinct business rules; no new exchange
rate or date conversion is inferred by this slice.

Limited extras have independent reservation counts. Legacy proof submission can
freeze an order and reserve capacity before a human confirms it. The rewrite must
keep reservation policy separate from confirmed money; copying that legacy paid
shortcut into the new ledger would be wrong. Cart editing, cancellation, reservation
expiry and financial confirmation require their own reviewed vertical slices.

Legacy order-by-ID HTML access must not be copied as an authorization model.
Knowing an order/event ID never grants another participant's private order data.

## Selected first slice

Store one bounded, typed, versioned orders catalog for each event. Configure it in
PostgreSQL through the administrator API and equivalent exact-approved agent tools,
with current version checks and immutable operation receipts. Reuse the existing
prepare/status/reconciliation pattern; no separate workflow platform is needed.

The initial catalog has stable identifiers, en/ru labels, exact integer minor-unit
base prices and one explicit currency/scale. Meal/service availability windows
and exclusive sales closing instants belong to this event. Required supplemental
costs must be disclosed; otherwise prices are explicitly base prices, not quotes.
Bound document/item counts and encoded bytes; a small whole-catalog read needs no
new search/index infrastructure.

Owner reads take an explicit event ID. The agent can first use the existing event
context resolver. Check the current account binding, deployment scope, enabled
`orders` module and assigned pass in the same transaction as the read. An assigned
unpaid or overdue pass qualifies; a waitlist record alone does not. Global admin
rights must not silently turn an owner read into a participant directory.

An entitled owner may still read a closed catalog with `open=false`. Represent
service/deadline instants in the event's named IANA timezone with the offset for
each instant, including DST. Neither an open window nor a catalog price promises
capacity, reserves a service, submits an order or confirms money.

## Planned acceptance

Behavior checks cover owner/assigned-unpaid access, waitlist/foreign/revoked denial,
disabled modules after event selection, different event catalogs, deadline equality,
DST, invalid prices/document bounds, stale edits and original receipt replay after
a later catalog revision. All writes require the same permissions through operator
and agent APIs. Fresh correctness, quality and privacy review loops precede native
owned IAM/Loom/Telegram catalog reads, module-disable denial and restart persistence.
No production menu or settings are read or changed without a separate authorized scope.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
