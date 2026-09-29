# Orders API: operator acceptance contract

These endpoints use the authenticated Core principal. A JSON body cannot choose
the acting owner. Synthetic QA can use `SandboxClient` from
`platform/scripts/fqa/client.mjs`, with its own loopback API address and stand
signing key. Production uses the configured identity provider, not QA tokens.

## Reads

- `GET /v1/order-events/{event}/orders?cursor=...`: own orders, with
  `{orders:[...],next:"..."}`. Follow `next` until empty.
- `GET /v1/order-events/{event}/payment-inbox?cursor=...`: the current principal's
  authorized payment-review queue, in the same page shape.
- `GET /v1/order-events/{event}/orders/{order}`: own order, scoped to the event.
- `GET /v1/orders/{order}`: own order using its original event, independent of
  the bot's currently selected event.
- `GET /v1/order-events/{event}/history`: own order change history.

An order includes `id`, `event_id`, `owner`, `version`, `choice`, `state`,
`attempt`, optional `attempt_at`, `proof_file`, `payment_admin`, `country`, and
`created_at`. Treat IDs and attempts as opaque strings. Use the values from
the current own order or authorized inbox; do not reconstruct them from labels.

## Quote, create and edit

Core quote: `POST /v1/order-events/{event}/quote` with a choice object.
Core writes: `POST /v1/order-actions` with `event_id`, `name` (`create` or
`edit`), `version`, `key`, `origin: "manual"`, and `choice`. For edit, also
send the observed `order_id` and current version. Creation uses version 0.
The authenticated principal owns the operation; do not put an owner in the body.

A choice accepts `customer`, `customer_first_name`, `customer_last_name`,
`customer_patronymus`, `extras` (selected catalog names as object keys), and
`days` (date keys to `{mealtimes:{MEAL:{dishes:[{name,count}]}}}`). Use actual
catalog keys. An extra is selected by its key's presence, not its value; remove
the key to deselect it. Client price and total fields never override server calculation.

The signed Telegram Mini App uses `Authorization: tma INIT_DATA`, where
`INIT_DATA` is current Telegram Web App init data signed with that stand's bot
token. Browser sessions use the normal consent flow and same-origin checks.
The Mini App endpoints are:

- `GET /miniapp/api/orders/{order}` returns `{order,event,editable}`.
- `POST /miniapp/api/quote?order_id={order}` takes the choice object itself.
- `POST /miniapp/api/orders/{order}` takes `{version,key,choice}`.

Use current order IDs and versions, preserve the complete original command for
exact replay, and choose a new key for a new edit. The Mini App assembles the
customer display name from its separate name fields. Under a configured public
mount such as `/bot`, prepend that mount to Mini App routes. Core API addresses
remain those of the private API service.

## Payment continuation

POST `/v1/order-actions` with JSON. The acting principal is the owner for
`cancel_proof`, or the authorized reviewer for `accept` and `reject`.

```json
{
  "event_id": "ACTUAL_EVENT",
  "order_id": "ACTUAL_ORDER_ID",
  "name": "accept",
  "version": 1,
  "attempt": "ACTUAL_CURRENT_ATTEMPT",
  "key": "OWN_UNIQUE_OPERATION_KEY",
  "origin": "manual"
}
```

Replace the sample version with the observed current version. Other command
names in this continuation scope are `reject` and `cancel_proof`. Manual origin
identifies this operator interaction; it does not bypass authorization. Never
mark an arbitrary agent action as manual to obtain additional permission.

For exact replay, repeat the complete original command, including key, version
and attempt. For a new action, use a new key and read current state first. A
conflict or failure is not proof that an earlier uncertain action rolled back;
read state and preserve the original command for reconciliation. The current
Go command contract is separate from historical Python callback formats.

For a new cash request after rejection, the order owner sends a new command to
the same endpoint with `name: "cash"`, the current order version, a new operation
key, `origin: "manual"`, and `payment_admin` containing the selected authorized
administrator's Core owner ID. Preserve the current `attempt` value from the
order if present; an empty historical attempt is not a reusable identity. A
successful cash request returns a new nonempty attempt. Subsequent review must
use that returned attempt and version; an old empty-attempt approval must fail.

## Isolated runtime

Use the topology in `product-sandbox.md` with a distinct Compose project,
loopback ports and private database. To test a frozen candidate, replace the
runtime `build` entries with the supplied immutable image and verify its ID.
Run `migrate` against the owned database before starting the app. Do not run
`product-fixture` over an imported dataset unless its documented seed identities
and event fixtures are deliberately part of the acceptance setup. A CLI-only
import acceptance does not establish rendered Telegram acceptance.
