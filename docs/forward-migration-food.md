# Forward migration: legacy food and activities

This slice is built for isolated candidate22. Independent Code QA and Functional QA are pending; this is not an acceptance claim.

## Source mapping

The source is `zns-chatbot/plugins/food.py`, `MenuHandler` in `zns-chatbot/server.py`, `templates/menu.html`, `static/menu.mjs`, and the captured `static/menu_2025_1.json`. The active routes are the eleven `food|` callbacks, `/exportfoodorders`, and GET/POST `/menu`. `/food_get_orders` is not registered and is not added.

One record is selected by `(user_id, pass_key)`. Its meal and activity payments are independent. Meal deletion calls `save_order` with empty meal choices; it does not delete activities. Source absence/null/false distinctions and additional metadata remain in the private raw provenance. Source `origin_info` is preserved there as message/chat hints; it never grants access to another chat.

| Source facts | Runtime representation |
| --- | --- |
| `_id`, `user_id`, `pass_key` | Durable same-bot source reference to a Core owner/event/order |
| `order_details`, `total`, `is_complete` | Saved meal choices, original RUB total, completeness |
| `activities` | Separate activity selections and source activity price policy |
| `payment_status`, `proof_file`, receipt/confirmation/rejection fields | Meal payment generation and immutable proof bytes |
| `activities_payment_status`, `activities_proof_file`, corresponding dates/reviewers | Independent activity payment generation and proof bytes |
| `proof_admin` | Saved common routing preference; new runtime receipts record their actual receiver |
| `notification_first_sent`, `notification_last_sent` | Imported meal reminder delivery markers |
| Pass `notified_food_first`, `notified_food_last` | Separate no-order reminder delivery markers |

The `legacyfood` runtime domain owns these semantics instead of combining them into a current `orders` payment. It reuses Core users, pass events, and immutable owner-bound proof byte storage. Food proof reads use food ownership and current food review grants; they never use the orders proof authorization function.

## Source policies

Meals retain day keys and zero-based menu item indices. `no-lunch` is complete with zero price. `individual-items` sums selected menu prices, including an empty list. Complete `combo-with-soup` costs 665 RUB; complete `combo-no-soup` costs 555 RUB. Missing required combo components make the meal incomplete and add no combo price. Dinner is optional. The import retains historical totals and requires a captured catalogue identity for the saved indices; it does not replace old totals with a new quote.

Activities are `open`, `yoga`, `cacao`, and `soundhealing`. Source `all` and `classes` replace the selection. Party plus any class costs 2500 RUB, party alone 2000, all three classes 2000; otherwise yoga costs 750, cacao 1000, soundhealing 1000. Cacao capacity is 38 selected records, including unpaid selections. Runtime changes hold one event lock so concurrent selection cannot oversell.

Meal and activity `paid`/`proof_submitted` states protect only their own choices. The source activity toggle handler omitted this check despite hiding selection controls in protected states. The Go domain closes that bypass deliberately; it does not reproduce the ability to change already submitted or paid activities through an old button.

Python does not store a separate historical receipt receiver. Import therefore leaves that field unknown instead of copying the mutable shared `proof_admin` into history. Confirming and rejecting actors remain exact. An editable meal save clears current shared routing as Python does, without changing the saved activity receipt history.

Every replacement receipt creates a new host-owned generation. Imported generation zero retains original provenance; its number is not presented as a historical Python token. Old admin callbacks lack an attempt token and may resolve only the still-current imported generation. They cannot approve or reject a replacement receipt. New buttons bind event, owner, order version, kind, and generation before effects.

## Export and reminders

`/exportfoodorders` requires the source food export role mapped to current event scope. It produces both source CSV outputs: all-order identity/payment/meal detail and a paid meal aggregate where the source confirmation field exists. Python includes an explicit null confirmation field in this aggregate, but prints “Нет” in the detail confirmation column; absence excludes the meal from the aggregate. Imported raw provenance preserves this distinction. Later payment generations use their own confirmation state. Existing orders XLSX stays separate.

The first reminder window is after deadline minus `notification_first_time` and before deadline minus `notification_last_time` minus `notify_after`. The last window is after deadline minus `notification_last_time` and before deadline. Pending positive-total meal orders are eligible only after the quiet period since `last_updated`. Separate no-order reminders cover assigned/paid pass holders whose meal record is absent or has empty unprotected details. Imported sent markers suppress delivery; unsent reminders are not marked delivered by import.

## Browser and message safety

Old `/menu` links keep `pass_key` and optional source `order_id`. Authentication derives the owner before any saved data is returned. The unauthenticated GET renders only a shell. Original message/chat IDs are hints, never grants to read another order or edit another chat. Pending payment prompts are intent hints; unrelated text and media continue through normal agent interpretation.

Already-loaded Python pages may still POST raw meal JSON to `/menu` with signed
`initData` in the query. This adapter verifies that signature, derives the owner,
and binds a hash of the launch/event/payload to one saved command/version before
effects. Only the hash is stored, never plaintext `initData`. Exact retries reuse
that binding and recheck current access. If a later edit is known, the old bound
request returns a stale conflict and asks the user to reload. The old wire has no
version or independent request nonce: a previously unseen old payload cannot be
distinguished from a newly intended save. The adapter does not claim freshness
in that ambiguous case. Reloading obtains the current editor, which sends an
explicit version and independent operation key.

The editor includes the original dish names, ingredients, weight, nutrition and
photos. Thirteen existing public photo assets are embedded unchanged; their
source/target paths and SHA256 hashes are recorded in
`qa.local/orders-import-build/food-photo-manifest.json`. Only embedded photo
filenames have public routes. CSV text beginning with spreadsheet formula
characters is escaped as text, a deliberate safety correction to the source.

## Implementation and reversal

Migration056 is additive. Existing orders/pass rows and proof bytes are not rewritten. The importer retains private source hashes, exact snapshot receipts, dependency mappings, and target snapshots for apply replay and reconcile. Runtime is stopped during bulk apply. Reversal before runtime resumes removes only the exact recorded inserted food rows in dependency order; existing user/event/proof data must not be deleted. Once new runtime actions occur, reconcile must report drift rather than overwrite them.

## Offline exporter contract

Use the version1 manifest and private staging procedure from `forward-migration.md`.
All domain coverage entries remain required. Food records use the existing `orders`
source, effective configuration uses `configuration`, and reminder markers remain
in `passes` or embedded event fields in `users`. Import users, events, then passes
before food. Modern order records without `pass_key` are explicitly excluded from
this food plan. Records carrying another valid `bot_id` are excluded; absent
`bot_id` on food records is scoped by the captured collection and manifest bot.

Export the exact menu bytes as a `configuration` file with `kind: "resource"`.
Keep array order, indices, names, prices and optional dish metadata unchanged.
Configuration JSONL must include an exporter-attested record of this shape:

```json
{
  "_id": "food-settings-synthetic",
  "kind": "legacy_food",
  "event_key": "synthetic_food",
  "menu_file": "menu.json",
  "menu_sha256": "<sha256 of exact menu.json bytes>",
  "deadline": "2027-06-01T12:00:00Z",
  "meal_prices": {"with_soup": 665, "without_soup": 555},
  "activity_prices": {"party": 2000, "party_and_classes": 2500, "all_classes": 2000, "yoga": 750, "cacao": 1000, "soundhealing": 1000},
  "cacao_capacity": 38,
  "first_before_seconds": 604800,
  "last_before_seconds": 86400,
  "notify_after_seconds": 3600,
  "admins": [{"user_id": 102, "can_export": true, "can_review": true, "can_assign": true, "instructions": {"en": "Synthetic transfer instructions", "ru": "Тестовые реквизиты"}}]
}
```

Amounts are decimal RUB, not kopecks. Assign `can_export` from `food.admins`;
`can_assign` from effective `payment_admins` (falling back to `admins` only when
the source does); `can_review` from effective payment admins plus
`payment_admins_old`. All users and reviewers need same-bot imported Core links.
These grants are separate from global administrative roles. Capture localized
payment instructions from the source admin metadata; do not infer bank details.

A complete synthetic menu resource can be:

```json
{"friday":{"lunch":[{"title_ru":"Суп","title_en":"Soup","price":185,"category":"soup"},{"title_ru":"Основное","title_en":"Main","price":250,"category":"main"},{"title_ru":"Гарнир","title_en":"Side","price":100,"category":"side"},{"title_ru":"Салат","title_en":"Salad","price":130,"category":"salad"}],"dinner":[{"title_ru":"Ужин","title_en":"Dinner","price":300}]}}
```

Food JSONL contains original source fields, for example:

```json
{"_id":{"$oid":"0123456789abcdef01234567"},"user_id":101,"pass_key":"synthetic_food","created_at":{"$date":"2027-05-01T10:00:00Z"},"last_updated":{"$date":"2027-05-02T10:00:00Z"},"order_details":{"friday":{"lunch":{"type":"combo-with-soup","items":{"soup_index":0,"main_index":1,"side_index":2,"salad_index":3}},"dinner":[0]}},"total":965,"is_complete":true,"activities":{"open":true,"yoga":true,"cacao":false,"soundhealing":false},"proof_admin":102,"payment_status":"proof_submitted","proof_file":"synthetic-meal.pdf","proof_received_date":{"$date":"2027-05-02T11:00:00Z"},"activities_payment_status":"paid","activities_proof_file":"synthetic-activity.jpg","activities_proof_received_date":{"$date":"2027-05-02T11:30:00Z"},"activities_payment_confirmed_by":102,"activities_payment_confirmed_date":{"$date":"2027-05-02T12:00:00Z"},"notification_first_sent":true,"notification_last_sent":false}
```

Other canonical lunches are `{"type":"no-lunch"}`,
`{"type":"individual-items","items":[0,"1"]}`, and
`{"type":"combo-no-soup","items":{"main_index":1,"side_index":2,"salad_index":3}}`.
Missing lunch and null/missing combo components preserve incompleteness. Empty
individual and dinner arrays are valid. Index strings and integers are accepted;
out-of-range or unknown shapes require correction of the source contract, never
silent removal of a chosen item. The stored historical `total` is preserved
even when it differs from the captured menu's current quote.

Payment status is absent/null, `proof_submitted`, `paid`, or `rejected`. Each side
has independent optional `proof_file`, `proof_received_date`,
`payment_confirmed_by/date`, and `payment_rejected_by/date`; prefix every field
with `activities_` for activity payment. Preserve absent dates and actors rather
than filling them from another side or current time. All source dates must have
an attested UTC offset and at most microsecond precision. Unknown source fields
block apply and remain visible in the private plan.

Every present proof file requires a manifest disposition. `owner_id` references
the source user record `_id`, not its Telegram numeric ID. `telegram_file_id`
must equal the exact stored food proof reference including its extension; it is
not a newly guessed Telegram file ID. Example:

```json
{"source":"orders","record_id":{"$oid":"0123456789abcdef01234567"},"owner_id":"synthetic-user-101","field":"proof_file","telegram_file_id":"synthetic-meal.pdf","blob":"proofs/meal.bin"}
```

The blob has its own manifest SHA256/size. When bytes genuinely cannot be
exported, replace `blob` with `"unavailable":true`; this preserves the reference
and explicitly prevents a false byte-preservation claim. Do not invent proof-message or proof-chat IDs: receipt submission does not
store them. The separate `origin_info` object records meal-editor refresh hints,
not the receipt origin. Proof storage creation time is an
import-time fact; it does not fill an absent historical receipt date.

Pass markers are strict booleans `notified_food_first` and
`notified_food_last`. Preserve dedicated and embedded records, including shadowed
embedded provenance. Effective dedicated registrations take precedence, as in
the pass importer. Food completion checks both dedicated-pass and user-food
deferrals against source evidence. A pass import alone leaves food pending.

## Synthetic CLI sequence

Use a disposable local PostgreSQL database with all platform migrations. The
snapshot must contain synthetic user/event/pass dependencies and complete
manifest coverage before the following commands. Run from `tools/migrate`:

```powershell
go run ./cmd/zns-migrate stage --snapshot C:\synthetic-food\snapshot --out C:\synthetic-food\stage
# Apply the reviewed users, events and passes plans first.
go run ./cmd/zns-migrate plan food --stage C:\synthetic-food\stage --out C:\synthetic-food\food-plan.json
```

Create a private resolution file with the exact plan artifact digest:

```json
{"version":1,"plan_sha256":"<food plan artifact_sha256>","dates_verified":true,"configuration_verified":true,"admin_grants_verified":true,"bot_namespace_verified":true,"writers_stopped":true}
```

These booleans attest an actual operator review of the exported facts; they do
not authorize invented values. Supply the disposable database DSN through
`MIGRATE_DATABASE_URL`, then:

```powershell
go run ./cmd/zns-migrate apply food --stage C:\synthetic-food\stage --plan C:\synthetic-food\food-plan.json --resolutions C:\synthetic-food\food-resolution.json
go run ./cmd/zns-migrate reconcile food --stage C:\synthetic-food\stage --plan C:\synthetic-food\food-plan.json --resolutions C:\synthetic-food\food-resolution.json
```

Apply commits the complete food snapshot atomically. Exact repeated apply reuses
the receipt. Changed source bytes, plan, identity mapping, attestations, existing
target rows or post-import target state refuse apply; no merge or overwrite is
performed. Keep runtime writers stopped until successful reconcile. This build
still requires the two independent QA gates before acceptance.

