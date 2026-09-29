# Offline orders import: supported modern slice

The removable importer now has `plan orders`, `apply orders` and
`reconcile orders`. This is a bounded modern-orders converter, not complete
legacy food/orders parity. Unknown or unsupported source fields block the whole
apply preflight before PostgreSQL is opened. No production import is authorized.

## Commands and dependency order

From `tools/migrate`:

```sh
go run ./cmd/zns-migrate plan orders --stage /private/stage --out /private/orders.json
go run ./cmd/zns-migrate apply orders --stage /private/stage --plan /private/orders.json --resolutions /private/orders-attestations.json
go run ./cmd/zns-migrate reconcile orders --stage /private/stage --plan /private/orders.json --resolutions /private/orders-attestations.json
```

Apply/reconcile use `MIGRATE_DATABASE_URL`. Apply accepted users and pass events
from the same snapshot first. The importer checks their durable source key and
record hash, selected-bot Telegram identity and stable Core owner. It neither
provisions identities nor infers owners from names, Mongo IDs or another bot.
The order catalog event key must match an imported pass-event reference from
the snapshot's selected bot namespace. Runtime event selection is separately
configured; this command never changes the running bot's active event.

Stop all runtime writers for apply and reconciliation. The private attestations
object must contain exactly these fields:

```json
{
  "version": 1,
  "plan_sha256": "<exact orders plan SHA-256>",
  "dates_verified": true,
  "configuration_verified": true,
  "admin_grants_verified": true,
  "bot_namespace_verified": true,
  "writers_stopped": true
}
```

These are operator assertions, not discovery or live verification. They cannot
override conversion blockers or introduce users/admins absent from the plan.
Legacy order records normally lack bot_id; namespace attestation explicitly
covers the selected event/collection. Artifacts contain private source records,
names and payment provenance. Stdout contains only counts, hashes and fixed
error codes. Use private directories and archive the exact plan/attestation
bytes. The plan is version 2; it retains full raw record JSON and source digests.
Its `apply_ready` remains false because offline planning cannot verify target
dependencies or operator attestations. Unsupported records have explicit blockers.

## Effective configuration export contract

Modern order configuration uses JSONL records. A shared configuration domain
may also contain validated food/massage records and a hash-bound food menu
resource with explicit owning-domain dispositions. Each modern catalog has:

- `_id`: a stable exporter record ID.
- `kind`: exactly `orders_catalog_v1`.
- `event_key`: the existing source event key.
- `deadline`: a verified RFC3339 instant, optionally inside `{"$date": ...}`.
- `byn_to_rub`: exactly 30, matching the current source and target runtimes.
  Another rate blocks import; the importer does not change currency policy.
- `menu`: the full effective menu JSON. Supported keys are `dishes`,
  `service_items`, `choices`, `category_labels` and `content_icons`.
  Known names, ingredients, icons, images, output, contents and service metadata
  remain intact. Unknown menu fields block rather than disappear.
- `extras`: a map of service keys to `price`, optional integer `capacity`
  and optional boolean `legacy`. Null booleans/capacities are not defaults.
- `transfer_instructions` and `transfer_instructions_localized`: effective
  secret-free business instructions; localized values are preserved.
- `payment_admins`: an explicit array of `user_id`, `country` (`be` or
  `ru`) and optional `region`. Each identity must already be imported in the
  same bot namespace. Duplicate admin identities block.
- Optional `payment_admin_ru`: the effective Python `config.orders.payment_admin_ru`
  value. Zero means disabled; a positive integer must name a `ru` administrator
  in `payment_admins`, with a same-bot imported identity. Negative/null values
  are invalid. Omission preserves the earlier contract but cannot resolve an
  implicit RU route. This field does not itself grant administrator access.

Export this from the deployed effective configuration and menu, including
historical events. Do not silently replace it with the repository's current
menu/deadline/constants, and do not include API keys. No Mongo or configuration
exporter is shipped in this slice. The operator must supply and attest the
reviewed export. Unknown configuration records/resources remain blockers.
Validated food/massage evidence retains explicit owning-domain dispositions;
the final whole-archive reconciliation must verify those owning imports.

## Supported source records

Modern orders require Mongo ObjectID identity, event_key, positive user_id,
created_at and a complete canonical choice. Optional updated_at is preserved;
absence uses creation time for the new target row. Order IDs are deterministic
`legacy-order:<bot>:<ObjectID>`. The constant bot prefix preserves source
ObjectID ordering for equal-time reservation priority.

Choices retain customer name parts, all days/meals, dish/service lines, extras
and totals. Unit prices are never recalculated from the current menu. Removed
historical dishes remain present. The converter checks exact cent precision,
line multiplication and aggregate totals against the target money bounds.
Fractional-cent or inconsistent source amounts block; they are not rounded.
Dates require lossless microsecond RFC3339 instants with explicit offsets.
Naive dates and unsupported Extended JSON number/date forms remain unresolved.

Supported payment states are unpaid, pending proof, cash and historically
validated paid. Existing nonempty payment_attempt_token is preserved. For an
actual-file proof with that field absent, the effective attempt identity is
exactly `legacy-proof:<source Telegram file ID>`, matching Python's
`payment_reservation_token`; it does not use the new Core proof ID. A present
empty token remains unresolved because source tokenless callbacks distinguish
absence from an empty field. Source validation is
preserved as historical state, not asserted as a fresh receipt validation.
proof_received, validated_at, cash_requested_at, source chat/message identifiers
and the original record remain in durable provenance. Effective attempt time
uses the first present instant in `payment_attempt_created_at`, `proof_received`,
`validated_at`, `created_at`, matching Python's `payment_attempt_time`.
`cash_requested_at` is not a reservation-priority fallback. Target proof creation
time uses proof_received when present, otherwise that effective source attempt
time; this fallback is not claimed to be an original receipt timestamp. Absent
source date/token fields remain absent in durable provenance. Cash is a sentinel
and never creates a receipt file. Tokenless cash remains tokenless in the target
until an authorized new approval creates a host payment identity. Historical
validation-only payments use the exact source validation identity described below.
A RU proof with no source `proof_admin` uses the explicitly exported positive
`payment_admin_ru`. Python `orders.py:724-738` saves the country and routes the
receipt through that configuration value without saving the admin in the order.
The importer materializes that effective route in Core while preserving the
source field's absence and the effective configuration in durable provenance.
This applies to pending and historically validated actual-file proofs. It never
assigns a route to an unrouted proof, cash or unpaid order. An explicit source
admin remains unchanged. Missing/disabled default configuration or another
implicit country route stays blocked; no admin is inferred from list order.

Every actual proof_file requires exactly one matching available manifest proof
with the same source order ID, field, Telegram file ID, owner Mongo ID, chat ID
and message ID. Blob bytes and their SHA-256 are rechecked. The new Core proof ID
is deterministic and owner-bound. The filename is a neutral `legacy-receipt`
label because this source contract has no original filename; no filename is
invented as historical evidence. Missing bytes, orphan proof entries or wrong
ownership/origin block the complete preflight. Total loaded proof payload is
bounded at 128 MiB in addition to snapshot/file limits.

## Canonical exporter shapes

JSONL files contain one compact object per line. Pretty-printed objects below
describe individual records; serialize each into one line before staging. Only
the documented fields are accepted. Omit absent optional fields; null is not an
empty string, empty object or empty array. Money is numeric BYN with at most two
decimal places. Counts are positive integers. `total` is reserved in extras.

A complete saved choice with one meal, a service item and an extra is:

```json
{
  "customer":"Synthetic Person",
  "customer_first_name":"Synthetic",
  "customer_last_name":"Person",
  "customer_patronymus":"",
  "days":{
    "2026-11-01":{
      "mealtimes":{
        "lunch":{
          "dishes":[{"name":"soup","count":2,"price":4.25,"total":8.50}],
          "service":{"items":[{"name":"container","count":1,"price":0.50,"total":0.50}],"total":0.50},
          "total":9.00
        }
      },
      "total":9.00
    }
  },
  "extras":{"shuttle":65,"total":65},
  "total":74
}
```

Name fields are optional strings. `days` and `extras` are required objects.
Every day requires `mealtimes` and its total. Every meal requires `dishes`
(array), `service` (object with `items` array and total), and total. Empty dishes
and service items use `[]`. No meals uses `"days":{}`. No extras uses
`"extras":{"total":0}`. Line names identify historical saved items; a name need
not remain in today's menu. Each line total equals count times price. Service,
meal, day, extras and order totals equal their corresponding child sums.

An unpaid record can contain just:

```json
{"_id":{"$oid":"100000000000000000000001"},"event_key":"demo_orders","user_id":101,"created_at":"2026-09-01T09:00:00Z","choice":{"days":{},"extras":{"preparty":35,"total":35},"total":35}}
```

The complete supported order field set is `_id`, `event_key`, `user_id`,
`created_at`, optional `updated_at`, `choice`, and these payment fields:

| State | Payment fields added to the base record |
| --- | --- |
| Pending uploaded proof | `proof_file` (nonempty Telegram file ID other than `cash`), `proof_chat_id` and `proof_message_id` (positive integers). Preserve original `proof_received`, `payment_attempt_token` and `payment_attempt_created_at` when present. Missing token/time uses the exact effective source fallbacks above. |
| Explicitly routed proof | The uploaded-proof fields plus `proof_country` (`be` or `ru`) and positive integer `proof_admin`. The admin must appear in the effective catalog with the same country. Omit both route fields for a proof awaiting route selection. |
| Configuration-routed RU proof | The uploaded-proof fields plus `proof_country:"ru"`, with `proof_admin` absent. Export the positive effective catalog `payment_admin_ru` and the corresponding RU `payment_admins` entry. The selected identity must exist in the same-bot users export. |
| Cash request | `proof_country:"be"`, positive `proof_admin`, plus either `proof_file:"cash"` or absent `proof_file` with `cash_requested_at`. Preserve `cash_requested_at`, `payment_attempt_token` and `payment_attempt_created_at` when present. Missing token remains empty in Core. No proof blob entry. |
| Historically paid | The proof or cash-origin fields plus `validation:true` and optional historical `validated_at`. This is imported validation, not a new review. |
| Validation-only paid | `validation:true`, optional historical `validated_at`, with no actual receipt file. Missing/empty/null/false `proof_file` or the `cash` sentinel is preserved as source provenance; no proof or payment method is invented. Missing source token uses Python's exact validation fallback below. |

All date fields use explicit-offset RFC3339 strings or `{"$date":"..."}`.
The original full record retains historical date fields in provenance. Pending
proof may omit `validation` or set it false. Unpaid rows cannot carry stale
payment attempts/dates in this slice. Empty-string `proof_file` is unpaid;
null/false proof forms are also supported for unpaid and historically validated
orders. A pending cash date together with present null/false/empty proof_file is
not the source's tokenless cash shape and remains unresolved.

### Tokenless cash continuation

An example is:

```json
{"_id":{"$oid":"100000000000000000000003"},"event_key":"demo_orders","user_id":101,"created_at":"2026-09-01T09:00:00Z","choice":{"days":{},"extras":{"shuttle":65,"total":65},"total":65},"proof_file":"cash","proof_country":"be","proof_admin":202}
```

Export administrator202 in the same-bot users domain and as a BE catalog admin.
This pending cash order does not reserve a limited service. Import preserves an
empty attempt and the source priority time. New Go admin commands can approve or
reject it only with normal current-version/admin authorization and a durable
original tokenless-cash source reference. Capacity-only changes may increment
the version without replacing its payment; refresh the card to act on that
current version. Rejection keeps normal unpaid/retry behavior. A later cash
request has its own new token, so old empty-attempt commands no longer match.

Approval is a new host action: it creates a host payment token, never inserts
that token into source provenance, and atomically claims capacity through the
normal runtime path. Priority retains a source attempt-created/proof-received
time when one exists; otherwise it uses the new approval time, matching the
source's newly written validated_at fallback. Exact retries reuse the committed
result. This does not make old Python-format buttons executable.

### Validation-only exporter evidence

Python `payment_reservation_token` uses
`legacy-validation:<str(validated_at)>` when the original token and actual proof
are absent. If validated_at itself is absent, the exact result is
`legacy-validation:true`, which needs no extra export metadata. If a validation
date exists, a normalized RFC3339 instant cannot always reconstruct Python's
original datetime spelling. Preserve the result of the source helper in this
reserved exporter-only object; it is not a source order field or an authorization
grant:

```json
{
  "validation":true,
  "validated_at":"2026-09-03T00:00:00Z",
  "_migration":{
    "payment_reservation_token":"legacy-validation:2026-09-03 03:00:00",
    "validated_at_source_offset":"+03:00"
  }
}
```

These fields supplement the base order, without a receipt, token or invented
cash origin. `_migration` accepts exactly `payment_reservation_token` and optional
`validated_at_source_offset`. For a naive source datetime, the offset is the
operator-verified mapping of that source clock to the exported instant (`Z` or
an explicit RFC3339 offset). For an aware source datetime, omit the offset and
preserve the offset already present in Python's helper output, for example
`legacy-validation:2026-09-03 03:00:00.123456+03:00`. Nonzero microseconds use
Python's six-digit spelling. The existing dates/configuration attestations cover
this exporter evidence; do not guess a timezone from the machine running import.

The converter checks validation state, original token/proof absence, exact
datetime spelling, equality to the exported validated_at instant, and exact
agreement with any selected-service slot token/time. It rejects the metadata
on other payment shapes. Missing evidence for a dated validation is unresolved;
arbitrary marker strings cannot authorize payment or invent source identity.
The full enriched export, including metadata and untouched source fields, is
retained in durable provenance. Validated orders without receipts remain paid
without creating a Core proof row.

A tokenless actual-proof example adds these fields to the base order (with its
original choice): `proof_file:"source-file"`, `proof_chat_id:101`,
`proof_message_id:42`, `proof_received:"2026-09-02T10:00:00Z"`. Omit
`payment_attempt_token` and `payment_attempt_created_at` when absent in source.
Its existing limited-service seat must carry
`reservation_attempt_token:"legacy-proof:source-file"` and
`reservation_attempt_created_at:"2026-09-02T10:00:00Z"`. The importer preserves
these slot fields; it never rewrites an inconsistent source reservation.

New Go cards bind the imported effective attempt and current order version in
their opaque command references. Those commands can continue the payment and
reject stale attempts normally. Compatibility with old Python-format Telegram
callback strings is a separate required cutover adapter; importing a source
record does not translate already-delivered Telegram buttons.

Each available actual receipt has this manifest entry, with `owner_id` equal
to the users document `_id`, not the Telegram ID or target Core owner:

```json
{"source":"orders","record_id":{"$oid":"100000000000000000000002"},"owner_id":"demo-user","field":"proof_file","telegram_file_id":"synthetic-receipt-id","chat_id":101,"message_id":42,"blob":"receipt.bin","unavailable":false}
```

Capacity records use stable exporter `_id` plus these exact shapes:

```json
{"_id":"demo-shuttle-0","event_key":"demo_orders","service":"shuttle","seat":0}
{"_id":"demo-shuttle-1","event_key":"demo_orders","service":"shuttle","seat":1,"reservation_id":{"$oid":"100000000000000000000002"},"reservation_attempt_token":"demo-attempt","reservation_attempt_created_at":"2026-09-02T10:00:00Z","reserved_at":"2026-09-02T10:00:01Z"}
{"_id":"demo-shuttle-2","event_key":"demo_orders","service":"shuttle","seat":2,"reservation_id":{"$oid":"100000000000000000000099"},"reserved_at":"2026-09-02T09:00:00Z"}
```

`seat` is a nonnegative integer. Free seats omit all reservation fields. Bare
claims have `reservation_id` and may have `reserved_at`, without payment-token
metadata; their order need not yet exist. Tokened claims carry the original token
and attempt instant. `reserved_at` is optional provenance, not a lease deadline.
For capacity3 export all three seats even when some are free. Unlimited extras
have no capacity rows.

## Complete synthetic CLI example

Use an empty private directory and your own migrated disposable PostgreSQL
database. The example intentionally uses synthetic identities and bytes. Its
identity attestations are only suitable for this synthetic database. It does
not provision real external identities. Run from `tools/migrate` in PowerShell7.
Set `MIGRATE_DATABASE_URL` separately to the disposable database DSN. The target
must already have platform migrations through051. For this synthetic example,
run `go run ./cmd/zns migrate` from `platform` with `ZNS_ENV=sandbox` and
`ZNS_DATABASE__URL` set to that same disposable database DSN. Set
`ZNS_AUTH__SIGNING_KEY` to a synthetic value of at least 32 characters, such as
`synthetic-migration-only-signing-key-123456`. Use an isolated
workspace `GOCACHE` if the default Windows cache is inaccessible. These sandbox
settings are for this example only, not the production migration procedure.

```powershell
$run = Join-Path $PWD ('orders-demo-' + [guid]::NewGuid().ToString('N'))
$snapshot = Join-Path $run 'snapshot'
$stage = Join-Path $run 'stage'
New-Item -ItemType Directory -Path $snapshot | Out-Null
$utf8 = [Text.UTF8Encoding]::new($false)
function Write-JSON($path, $value) {
  [IO.File]::WriteAllText($path, ($value | ConvertTo-Json -Depth 40 -Compress), $utf8)
}
function Write-Rows($path, $rows) {
  $lines = @($rows | ForEach-Object { $_ | ConvertTo-Json -Depth 40 -Compress })
  [IO.File]::WriteAllText($path, (($lines -join "`n") + "`n"), $utf8)
}
function Run-Import([string[]]$arguments) {
  $result = & go run ./cmd/zns-migrate @arguments
  if ($LASTEXITCODE -ne 0) { throw 'Importer command failed' }
  $result | ConvertFrom-Json
}
$user = @{_id='demo-user'; bot_id=77; user_id=1101; print_name='Synthetic Person'}
$event = @{_id='demo-event'; key='demo_orders'; finish_date='2026-12-01T00:00:00Z';
  title_long=@{en='Demo';ru='Демо'}; title_short=@{en='Demo';ru='Демо'};
  pass_types=@(@{amount=10;price=100;start='2026-09-01T00:00:00Z'})}
$catalog = @{_id='demo-catalog';kind='orders_catalog_v1';event_key='demo_orders';
  deadline='2026-11-01T00:00:00Z';byn_to_rub=30;
  menu=@{dishes=@{};service_items=@{};choices=@{}};
  extras=@{shuttle=@{price=65;capacity=3};preparty=@{price=35}};
  transfer_instructions='Synthetic only';
  transfer_instructions_localized=@{en='Synthetic only';ru='Только тест'};
  payment_admins=@(@{user_id=1101;country='be';region='Demo'})}
$paidID = @{'$oid'='100000000000000000000002'}
$unpaid = @{_id=@{'$oid'='100000000000000000000001'};event_key='demo_orders';
  user_id=1101;created_at='2026-09-01T09:00:00Z';
  choice=@{days=@{};extras=@{preparty=35;total=35};total=35}}
$paid = @{_id=$paidID;event_key='demo_orders';user_id=1101;
  created_at='2026-09-01T09:00:00Z';
  choice=@{days=@{};extras=@{shuttle=65;total=65};total=65};
  proof_file='synthetic-receipt-id';proof_chat_id=1101;proof_message_id=42;
  proof_received='2026-09-02T10:00:00Z';payment_attempt_token='demo-attempt';
  payment_attempt_created_at='2026-09-02T10:00:00Z';
  validation=$true;validated_at='2026-09-03T10:00:00Z'}
$cash = @{_id=@{'$oid'='100000000000000000000003'};event_key='demo_orders';
  user_id=1101;created_at='2026-09-01T09:00:00Z';
  choice=@{days=@{};extras=@{preparty=35;total=35};total=35};
  proof_file='cash';proof_country='be';proof_admin=1101;
  cash_requested_at='2026-09-02T11:00:00Z';payment_attempt_token='demo-cash';
  payment_attempt_created_at='2026-09-02T11:00:00Z'}
$slots = @(
  @{_id='demo-shuttle-0';event_key='demo_orders';service='shuttle';seat=0},
  @{_id='demo-shuttle-1';event_key='demo_orders';service='shuttle';seat=1;
    reservation_id=$paidID;reservation_attempt_token='demo-attempt';
    reservation_attempt_created_at='2026-09-02T10:00:00Z';reserved_at='2026-09-02T10:00:01Z'},
  @{_id='demo-shuttle-2';event_key='demo_orders';service='shuttle';seat=2;
    reservation_id=@{'$oid'='100000000000000000000099'};reserved_at='2026-09-02T09:00:00Z'})
Write-Rows "$snapshot/users.jsonl" @($user)
Write-Rows "$snapshot/events.jsonl" @($event)
Write-Rows "$snapshot/configuration.jsonl" @($catalog)
Write-Rows "$snapshot/orders.jsonl" @($unpaid,$paid,$cash)
Write-Rows "$snapshot/slots.jsonl" $slots
[IO.File]::WriteAllBytes("$snapshot/receipt.bin", [Text.Encoding]::UTF8.GetBytes('Synthetic receipt bytes'))
$specs = @(
  @('users.jsonl','users','records',1), @('events.jsonl','events','records',1),
  @('configuration.jsonl','configuration','records',1), @('orders.jsonl','orders','records',3),
  @('slots.jsonl','order_capacity','records',3), @('receipt.bin','files','blob',0))
$files = @($specs | ForEach-Object {
  @{path=$_[0];source=$_[1];kind=$_[2];records=$_[3];
    bytes=(Get-Item "$snapshot/$($_[0])").Length;
    sha256=(Get-FileHash "$snapshot/$($_[0])" -Algorithm SHA256).Hash.ToLowerInvariant()}
})
$domains = 'users','events','passes','orders','order_capacity','massage','messages','files','bot_storage','knowledge','schedule','configuration'
$coverage = @($domains | ForEach-Object {
  $included = $_ -in @('users','events','configuration','orders','order_capacity','files')
  @{domain=$_;name=('demo_'+$_);status=$(if($included){'included'}else{'absent'});
    reason=$(if($included){''}else{'Synthetic example contains no such source'})}
})
Write-JSON "$snapshot/manifest.json" @{version=1;snapshot_id='orders-demo';bot_id=77;
  captured_at='2026-09-26T12:00:00Z';consistency='stopped_writer';coverage=$coverage;files=$files;
  proofs=@(@{source='orders';record_id=$paidID;owner_id='demo-user';field='proof_file';
    telegram_file_id='synthetic-receipt-id';chat_id=1101;message_id=42;blob='receipt.bin';unavailable=$false})}
Run-Import @('verify','--snapshot',$snapshot)
Run-Import @('stage','--snapshot',$snapshot,'--out',$stage)
Run-Import @('plan','users','--stage',$stage,'--out',"$run/users-plan.jsonl")
$userRows = @(Get-Content "$run/users-plan.jsonl" | ForEach-Object { $_ | ConvertFrom-Json })
$userRow = @($userRows | Where-Object { $null -ne $_.candidate })[0]
Write-JSON "$run/users-resolution.json" @{version=1;
  plan_sha256=(Get-FileHash "$run/users-plan.jsonl").Hash.ToLowerInvariant();identity_attested=$true;
  users=@(@{legacy_key=$userRow.legacy.key;owner='demo-owner-1101';
    issuer='https://synthetic.invalid';subject='demo-subject-1101';can_book=$true})}
Run-Import @('apply','users','--stage',$stage,'--plan',"$run/users-plan.jsonl",'--resolutions',"$run/users-resolution.json")
Run-Import @('plan','events','--stage',$stage,'--out',"$run/events-plan.json")
$eventPlan = Get-Content "$run/events-plan.json" -Raw | ConvertFrom-Json
Write-JSON "$run/events-resolution.json" @{version=1;
  plan_sha256=(Get-FileHash "$run/events-plan.json").Hash.ToLowerInvariant();
  dates_verified=$true;configuration_verified=$true;admin_grants_verified=$true;
  events=@(@{legacy_key=$eventPlan.events[0].legacy.key;display_order=0})}
Run-Import @('apply','events','--stage',$stage,'--plan',"$run/events-plan.json",'--resolutions',"$run/events-resolution.json")
Run-Import @('plan','orders','--stage',$stage,'--out',"$run/orders-plan.json")
Write-JSON "$run/orders-resolution.json" @{version=1;
  plan_sha256=(Get-FileHash "$run/orders-plan.json").Hash.ToLowerInvariant();
  dates_verified=$true;configuration_verified=$true;admin_grants_verified=$true;
  bot_namespace_verified=$true;writers_stopped=$true}
Run-Import @('apply','orders','--stage',$stage,'--plan',"$run/orders-plan.json",'--resolutions',"$run/orders-resolution.json")
Run-Import @('reconcile','orders','--stage',$stage,'--plan',"$run/orders-plan.json",'--resolutions',"$run/orders-resolution.json")
```

To represent the source RU route in this example, add a second same-bot users
record for a synthetic administrator202, add
`{"user_id":202,"country":"ru","region":"Demo RU"}` to the catalog admins,
set catalog `payment_admin_ru` to202 and add `proof_country:"ru"` to the paid
proof order while leaving `proof_admin` absent. Include the second user's
explicit identity resolution in the users apply. Recompute manifest counts and
hashes before staging. Do not change a staged bundle or an existing plan.

For production-shaped exports, replace the empty synthetic menu with the full
effective catalog. `menu.dishes` and `menu.service_items` are objects keyed by
item name; each item has numeric `price` and optional strings `name_en`,
`name_ru`, `ingredients_en`, `ingredients_ru`, `output`, `image`, `kind`, `icon`,
plus optional string arrays `contents` and `service`. `menu.choices` maps day to
mealtime to category to an array of item names. Optional `category_labels` and
`content_icons` map keys to locale/string objects. No source fields should be
discarded to force an export into this supported shape.

## Capacity and transactions

The entire source seat inventory must be included, even free slots. Each limited
service needs exactly seats 0 through capacity-1. Duplicate seats/reservations,
unknown services and inconsistent tokened claims block. A tokened claim must
refer to a service selected in its matching paid/proof order. Paid/proof choices must
already own matching source reservations; the importer never allocates new
seats, sorts owners into seats, adopts attempts or calls runtime reconciliation.
Bare in-flight claims are retained, including claims whose source order is not
present. Tokened claims require their matching order, payment state, attempt
token and attempt time. See [the runtime contract](order-capacity.md).

Each event catalog, admins, orders, proof bytes, exact seat rows, durable source
references and apply receipt commits in one transaction. A failure rolls back
that event; earlier events remain committed. Migration051 retains domain,
source key/hash, source record and target identity in Core after tool removal.
The temporary `migrate_import.order_receipts` table retains exact plan/resolution
digests, resolved owner mapping and a whole-event target snapshot.

Exact apply replay checks every imported target row, proof byte, reservation,
source reference and admin set without overwriting. Newly added orders, slots
or admins also cause drift failure. Existing target events are conflicts even
when values match; the importer never merges. Partial resume requires the same
plan and attestation bytes. Concurrent exact runs serialize on the source
catalog identity and either commit or verify the existing receipt.

`reconcile orders` performs only verification and takes no repair action. It
does not create receipt schema, target rows or missing event imports. Missing
receipts fail. Reconciliation is per event under its transaction; it is not a
global snapshot while runtime writers continue.

## Explicit remaining blockers and acceptance

Validated legacy `food.py` records and legacy food/massage configuration remain
in the order plan with an explicit `delegated_domain`. Their original source
key, raw record and record hash stay intact. Food menu resources and payment
proofs have explicit `domain_dispositions`, bound to the owning source record
hash and exact resource/proof digest (including the receipt blob digest when
available). Only the owning converters can recognize these records; mixed
modern/legacy fields, unknown kinds, unreferenced resources and unsupported
proof fields still fail closed. Modern order preparation reuses the food proof
validator for owner linkage, duplicates, missing proofs and blob integrity.

Modern apply/reconcile counts and receipts cover only modern orders. Delegation
does not claim that food or massage was imported. Whole-archive completion must
match every delegated source key/hash and resource/proof disposition to the
nonexcluded owning plan under the same manifest, then successfully reconcile
that exact owning plan and its receipt. Missing owning receipts, changed source
hashes or failed owning reconciliation leave the archive incomplete. The
synthetic full-import rehearsal records those links before issuing completion
evidence. Modern-only plan bytes remain unchanged because additional fields
are omitted when unused. No new runtime or receipt schema is introduced.

Unknown fields,
capacity notices/reminder-delivery state, tombstones, unresolved validation-token
clock evidence, unresolved administrator routing and historical rejected-payment
metadata do not yet have complete converters. They stay in the private plan
and block apply. Physically absent deleted orders cannot be recovered from a
snapshot; no deleted history is invented.

Synthetic PostgreSQL verification covers actual dependent users/event imports,
historical removed dishes and cent totals, validated payment/cash states,
owner-bound proof bytes, source seat preservation, immutable replay, drift,
partial resume, concurrent apply and refusal of unsupported data before SQL.
These implementation tests are not the fresh independent Code/Functional QA
gates and do not establish complete migration parity or production readiness.

