# Imported Python order callbacks

The Go bot accepts the active Python `orders|` callback forms after offline order import. The importer must finish and reconcile before the bot resumes delivery. Imported Mongo ObjectIDs remain source identities; they are not Core order IDs.

## Deployment namespace

Core obtains the legacy bot namespace from `auth.zitadel.bot_id`. Outside Zitadel mode, it can instead use a positive numeric prefix from the trusted configured Telegram token. When both values exist, they must agree. A mismatch fails startup. With neither value, legacy resolution is disabled while ordinary sandbox commands remain available. For a synthetic deployment, configure namespace `77` and a matching fake token prefix if a prefix is used.

The callback, request body, chat, and source document cannot choose this namespace. Bot database access remains limited to its own delivery tables; Core resolves imported references through the authenticated API.

## Callback contract

`<id>` is the imported order's 24-character hexadecimal Mongo ObjectID. `<admin>` is the original payment administrator's positive Telegram user ID, mapped through the same configured bot namespace. `<token>` is the original Python payment attempt token, not a Core proof ID or a newly generated replacement.

| Callback | Behavior and access |
| --- | --- |
| `orders|start` | Show the actor's orders for the configured current order event. |
| `orders|close` | Close the old message and remove its keyboard. |
| `orders|xlsx` | Export the current event through the existing orders exporter; current event administrator access is required. |
| `orders|pay|<id>` | Show current payment instructions for the actor's imported order. |
| `orders|del|<id>` | Delete the actor's eligible unpaid/cash order using current domain deadline and payment rules. |
| `orders|cash|<id>|<admin>` | Start a cash request to a current Belarus payment administrator. |
| `orders|paid|<id>` | Route an existing proof to the imported effective RU administrator. This requires imported configuration and current administrator membership. |
| `orders|adm_acc|<id>[|<token>]` | Accept the matching pending payment using current event administrator access. |
| `orders|adm_rej|<id>[|<token>]` | Reject the matching pending payment using current event administrator access. |
| `orders|pcancel|<id>[|<token>]` | Cancel the actor's matching actual uploaded proof. Cash requests are not proof cancellation targets. |

The bracketed token segment is optional. An omitted or empty segment is valid only when the original source document had no `payment_attempt_token` field and the current payment still represents that imported attempt. A source field containing an empty string or null is not an absent field. A tokened callback must match both the original source token and the current attempt. Retrying cash or submitting a new proof makes an old payment button stale.

For example, a synthetic original button may contain:

```text
orders|pay|0123456789abcdef01234567
orders|cash|0123456789abcdef01234567|202
orders|adm_acc|0123456789abcdef01234567|abc123short
orders|pcancel|0123456789abcdef01234567
```

Only registered forms of at most 64 bytes are accepted. Unknown actions, extra segments, invalid IDs, foreign owners, missing or ambiguous same-bot import references, and unavailable payment context are refused without changing the order. Core IDs cannot be substituted for Mongo ObjectIDs.

## Delivery and authorization

Before effects, the bot stores an immutable binding for the authenticated owner and Telegram update ID. It includes the original callback digest, resolved event/order, current order version, payment attempt, and command. Retries reuse this binding and the same command idempotency key. They cannot pick up a later order version or follow a changed process event default. Reusing a delivery ID with different callback data is refused.

Core checks current permissions when resolving a callback and again when executing the resulting command. Navigation and export also refresh read access on retries. Saved delivery state never grants permanent access. Order callbacks resolve the event from their imported order; navigation callbacks without an order ID bind the configured event on their first delivery.

Accepted actions use the existing Go order cards, payment instructions, notifications, and XLSX exporter. Close and error messages support English and Russian. Source export authorization is explicit: Python `orders.py` permits configured order administrators, its effective RU administrator, and Belarus payment administrators. The Go adapter requires the corresponding current event administrator membership; the callback name itself grants no permission.

## Source boundary

The active source registrations and handlers are in `zns-chatbot/plugins/orders.py`: token filters near lines 143–190; delete/pay/cash near 495–627; start/close/paid near 629–775; accept/reject/proof cancellation near 777–935; XLSX near 995; registration near 1238–1252. Raw imported provenance is preserved by the offline importer and is not rewritten by callbacks.

This compatibility adapter covers `orders|` callbacks. Legacy `food|` callbacks are a separate migration slice. It does not make an unimported source order available or replace the offline importer and its prerequisite validation.
