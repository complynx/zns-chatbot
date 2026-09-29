# Legacy food and activities parity scope

Source inspection: 2026-09-26. This is an implementation contract for the next
migration slice, not implementation or acceptance evidence.

## Active entry points

`zns-chatbot/plugins/food.py:1501` registers `exportfoodorders` and `food|…`
callbacks. The `/food` and `/activities` entry commands are commented out, but
existing messages, records and callback routes remain active.
`zns-chatbot/server.py:869` registers GET/POST `/menu`. The adjacent
`/food_get_orders` registration is commented out; its class alone does not make
it an active endpoint. Do not reintroduce that endpoint solely for parity.

The callback dispatcher at food.py:253 resolves these existing suffixes:
`pay`, `adm_acc`, `adm_rej`, `exit`, `delete_order`, `toggle_activity`,
`submit_activities`, `activities_exit`, `activities_pay`,
`adm_activities_acc`, `adm_activities_rej`. Inspect each original handler and
its button construction before implementation; new Go names alone are not
compatibility with already delivered Telegram callback data.

### Legacy callback wire contract

Original Telegram buttons carry these exact pipe-separated payloads. `<id>` is
the original food record's Mongo ObjectId string, not a Go row number. Resolve it
within the imported bot namespace and current actor's permissions. Payloads do
not gain authority merely because they came from an old message.

| Action | Original callback data |
| --- | --- |
| Meal payment | `food|pay|<id>` |
| Approve meal proof | `food|adm_acc|<id>` |
| Reject meal proof | `food|adm_rej|<id>` |
| Exit meals | `food|exit` |
| Delete meal selection | `food|delete_order|<id>` |
| Toggle activity | `food|toggle_activity|<activity>` |
| Submit activity selection | `food|submit_activities` |
| Exit activities | `food|activities_exit` |
| Activity payment | `food|activities_pay|<id>` |
| Approve activity proof | `food|adm_activities_acc|<id>` |
| Reject activity proof | `food|adm_activities_rej|<id>` |

Activity values are `open`, `yoga`, `cacao`, `soundhealing`, `all`, and `classes`.
The activity selection callbacks have no order-ID argument; resolve the actor's
current event selection. Preserve the documented legacy payment-generation and
current-authorization rules for administrative callbacks. Source contract:
original button construction in `zns-chatbot/plugins/food.py`, with dispatcher
splitting on `|` and passing trailing fields in order. `/menu` is a browser route,
not a Telegram text command. `/exportfoodorders` is the registered export command;
an event argument is not part of the original command contract.

## Data and business boundaries

One source food record is selected by user_id and pass_key. It can contain both
meal and activity selections with independent payment states and receipts.
`payment_status` and `activities_payment_status` must not collapse into one
state. Preserve each proof, original timestamp, reviewer, rejection and unknown
field independently. Source absence/null/false/empty distinctions remain in
provenance; no fabricated proof, acceptance, actor or historical token.

Meal choices use the source menu catalogue's day keys and item indices;
`static/menu_2025_1.json` is loaded by food.py:1640. Preserve the catalogue identity
and historical prices when translating choices, including no lunch, individual
items, combo-with-soup, combo-no-soup and dinner. Record unsupported or ambiguous
source shapes as actionable plan conflicts, not silent empty choices.

Activities implement individual toggles plus `all` and `classes`; cacao has a
capacity restriction (food.py:1004). Preserve pending and protected payment
states independently of meal editability. Reuse current transactional domain
operations where they express these semantics; explicitly resolve missing
semantics instead of mapping both payments onto one current order by convenience.

Map food.admins, effective payment_admins fallback and payment_admins_old to
current scoped authorization without turning old callback possession into a
grant. A trusted imported source namespace and event binding are required for
legacy IDs. Follow the existing current-principal, durable callback binding and
replay rules; the bot must not query Core SQL.

## Browser and export behavior

`MenuHandler` loads pass_key/order_id and localized menu state. POST accepts
signed Telegram initData and meal JSON, validates the active event and derives
the owner from authentication. Paid/proof-submitted meal choices are protected.
Original message/chat IDs are refresh hints and must not grant access or allow
editing somebody else's message. Keep old links usable and enforce owner
access even where the old GET implementation was weaker. Free text and media
continue through agent intent; a pending receipt form must not trap the next
unrelated message.

`handle_export_orders_cmd` (food.py:2107) reads food records for the selected
event and includes Telegram/profile identity, meal payment/confirmation/total,
day-specific meal selections and aggregate food counts. It sends two CSV files:
`food_orders_*` for order details (food.py:2191), and `meal_summary_*` for
the meal aggregate (food.py:2397). Verify both filters and every output column;
the current `/exportfoodorders` alias to the modern order exporter alone does
not prove matching contents.

The original periodic notification sender is started at food.py:1508 and uses
first/last deadline windows and persisted sent markers. Account for those
markers and outstanding reminders during import; do not resend old notifications
or mark an unsent message delivered.

## Acceptance

- A private offline plan/apply/reconcile stage preserves both independent
  payment lifecycles, catalogue data, provenance and media. Exact replay,
  transaction failure, namespace ambiguity and source drift are tested with
  real PostgreSQL. The runtime has no importer dependency.
- Fresh Code QA compares original handlers and final diff.
- Fresh source-blind Functional QA exercises existing legacy callbacks and
  `/menu` through rendered EN/RU Telegram-like UI: independent meal/activity
  payments, editing, receipt replacement, admin changes, stale callbacks,
  retries/concurrency/restart, capacity, old links, export contents and reminders.
- Final real Telegram checks use only the designated test bot/account, after
  synthetic gates. No production cutover is implied.
