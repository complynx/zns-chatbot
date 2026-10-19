# Event orders catalog

This document records the original schema-23 catalog slice and its historical
acceptance below. Later components add complete quotes, saved selections and
submitted-unpaid orders. The current source also has the
[resource/demand foundation](shuttle-orders-capacity-foundation.md), using the
same versioned catalog in PostgreSQL. Its manifest is `mvp-41`; schema remains40.
The owned stand still runs the preceding `mvp-40` image. Actual four-path order
submission acceptance is recorded in
[the real QA report](shuttle-orders-submission-real-qa-2026-10-17.md).

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
These are independent windows, so on-site sales can overlap a service. Dates are
not inferred from a weekday or fixed Moscow/Minsk offset. Submicrosecond instants
are rejected rather than silently truncated by PostgreSQL. Service windows may
precede the main event (for example, a preparty); event scope is explicit.

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

## Developer and owned-stand acceptance

Architecture and Go consultations preceded implementation. Focused behavior tests
cover exact approval/publication, original replay after expiry/later edits, stale
event versions, owner access without payment, module disablement and account
revocation, plus money/reference validation and deadline/DST boundaries. An independent
review found a missing expired-before-first-apply assertion; that case now verifies
rejection without publication, separately from successful receipt replay. Three
review perspectives and a fresh final source pass found no remaining issues. The
administrative discovery-test update received a separate clean review. Lint is clean.

Focused acceptance caught a Huma schema-name collision between `orders.Document`
and `events.Document`; the new type is `orders.CatalogDocument`. The corrected real
API setup and behavior tests passed. An initial test-fixture compile error and the
schema failure are retained in local QA logs. The first complete race run then
failed only on the old administrative effect allowlist; the test now requires the
catalog read/write methods and checks their correct effects and ordinary-user denial.
Its complete rerun reached the 1.5 GiB test-container memory cap and was killed by
the cgroup OOM handler. This is a failed gate, not a pass. A fresh QA database and a
serial full race run with a smaller Go heap were tried without increasing the
container cap. The serial process was proactively stopped when memory again grew
to about 1.2 GiB, before another OOM. Final integration verification now uses fresh
processes for every 15 top-level tests, preserving race and concurrency checks.
The authoritative `go test -list` list has 159 tests in 11 groups; an exact coverage
manifest records selected and completed names. Other explicit packages are checked
with race separately. The first grouped run exposed a pre-existing backpressure
test race: client-side pipe consumption did not establish server-side admission.
The test now waits for actual handler body consumption before probing the competing
request, then verifies release after the stream finishes. Product code is unchanged;
five focused race repetitions and an independent review passed. The final grouped
gate passed all 159 enumerated integration tests in 11 fresh processes, with exact
coverage recorded and no OOM kills. Other explicit Go packages passed with race;
final lint reported zero issues. Earlier failed runs remain failures, preserved
separately from this successful bounded run. The native
application remained running at about 14–15 MiB.

Owned real IAM/Loom acceptance prepared normalized catalog arguments, suspended for
exact approval and applied them using Loom's generated operation UUID. An ordinary
owner could not prepare the administrative write. The assigned pass had zero
confirmed money; its owner read the catalog from the authorized Telegram chat
through a real Loom script. Disabling `orders` on that fixture event denied the
next HTTP and actual Telegram/Loom tool read. Re-enabling and restarting the bot
preserved both catalog and original administrative receipt; payment-review count
was unchanged. The pre-migration dump was verified with `pg_restore --list`.

The provider was the manual Codex mailbox. This proves real-service permission,
approval, tool and persistence paths for the exercised choices; it is not an
unattended model-quality evaluation. No production settings or real money changed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
