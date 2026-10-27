# Event orders catalog

This document records the original schema-23 catalog slice and its historical
acceptance below.
Later components add complete quotes, saved selections and
submitted-unpaid orders.
The current source also has the
[resource/demand foundation](shuttle-orders-capacity-foundation.md), using the
same versioned catalog in PostgreSQL.
Its manifest is `mvp-41`; schema remains40.

## Catalog configuration

Schema 23 stores one versioned JSONB document per event and separate immutable
five-minute proposals and operation receipts. Configuration belongs in PostgreSQL,
not init YAML. The document has one currency/scale, up to 100 items and 20 offers,
and at most 48 KiB compact encoded JSON. IDs are unique within their item/offer
kind; every offer references existing item IDs without duplicates. Both en and ru
labels are required. Prices are nonnegative exact integer minor units, at most
9,000,000,000,000,000; currency scale is 0 through 6.

An offer specifies absolute service start/end and absolute sales opening/closing.
Each window must increase; sales opening is inclusive and closing is exclusive.
These are independent windows, so on-site sales can overlap a service.
Dates are
not inferred from a weekday or fixed Moscow/Minsk offset.
Submicrosecond instants
are rejected rather than silently truncated by PostgreSQL.

All prices are base prices. Required packaging, cutlery and other future order
charges have no pricing engine in this slice. The API sets `prices_are_base=true`;
the agent must not present the listed amount as a final payable order total.
No capacity or current remaining places are exposed or implied.

## Administrative API and agent tools

These authenticated endpoints are also the operator API; operators use the same
exact proposal/confirmation protocol as the agent:

- `POST /internal/agent/administration/orders_catalog` reads configuration by event ID.
- `POST /internal/agent/administration/set_orders_catalog/prepare` freezes arguments.
- `POST /internal/agent/administration/set_orders_catalog` applies exact approved arguments.
- `POST /internal/agent/administration/set_orders_catalog/status` reconciles the operation.

The current global `zinusya.admin` role and nonrevoked local account are required.
Configuration reads/writes do not require the administrator to own a pass. A
disabled module may be configured before enablement. The deployment bot and
organization still constrain every event. Event-local administrators do not yet
receive this global configuration capability.

Read the event and current catalog first. A change contains `event_id`,
`expected_event_version`, `expected_version` (0 creates a missing catalog), and
`document`. A nonzero operation UUID and matching idempotency key bind the exact
normalized arguments to a proposal ID. The preparation discloses those arguments,
event/catalog versions and expiry. Applying locks the event and current account,
rechecks identity, versions and unexpired approval, then writes catalog and receipt
atomically. Changed retries conflict. A successful same-operation replay returns
its original receipt before later version/expiry checks; missing status is unknown.
No delete/reset-history operation is introduced. Populated rollback is guarded and
requires exporting catalogs, approvals and receipts before a deliberate migration plan.

## Participant read

`zinusya_orders.catalog` calls `POST /internal/agent/orders/catalog` with an explicit
`event_id`; it accepts no owner ID. The existing event-context resolver can first
resolve module `orders`. The read requires the current owner binding, this bot and
organization, an enabled event module and an assigned pass. Unpaid/overdue qualifies;
waitlisted alone does not. A global administrator gets no pass entitlement bypass
through this endpoint. A missing/foreign/disabled/unentitled catalog returns the
same 404 without exposing its content.

The owner check, module entitlement and document read share one repeatable-read
transaction and connection. The response includes current event/catalog versions,
event name/IANA timezone, observation time, bilingual items, offers and base-price
marker. Offer instants display each applicable event-zone offset, including DST.
`sales_open` reports only the current time window. Closing sales does not remove
read entitlement, and no catalog read changes registrations, money or capacity.

Manifest `mvp-22` added the static owner module and two dynamically discovered global
administration methods. Administration methods are hidden from ordinary users,
with permissions rechecked at execution. All content is untrusted data.

The subsequent [owner quote slice](shuttle-orders-quote.md) adds optional explicit
complete-pricing policy, mandatory supplement references and `quote` in manifest
`mvp-23`. The catalog response still marks item prices as bases and exposes that
policy when configured. Its historical catalog-only acceptance below remains scoped
to `mvp-22`; current quote acceptance is recorded in its separate contract.
