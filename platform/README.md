# Go platform — migration sandbox

The migration target is full parity with the active Python bot. This directory is
a staged implementation, not a production replacement. The existing Python
deployment is unchanged. See [the migration plan](../docs/go-migration.md).

Functional reviewers: start with the [FQA toolkit](scripts/fqa/README.md) for
preflight, public API access, owned fixtures/cleanup, mouse/touch Telegram
interaction, evidence traces and independent XLSX inspection.

## Run the manual sandbox

Requirements: Docker with Linux containers and Compose v2. From this directory:

```sh
docker compose up -d --build --wait
```

Open **http://localhost:8090**. On this Windows workstation, use
`docker --context desktop-linux compose ...` if your default context differs.

The shared image is built once by the `api` service. PostgreSQL, schema bootstrap,
API, polling bot, fake Telegram and scripted model start together. All passwords
and identities are disposable fixtures. No real Telegram/model credentials are
read. Every executable refuses to start without `ZNS_ENV=sandbox`.
The bot, API and model have only an internal network. Fake Telegram and PostgreSQL
also have a host-facing network to support Docker Desktop port forwarding; only
loopback ports 8090 and 55432 are published. Do not expose this sandbox publicly.

### Audio/video test stand

The full local quality gate also exercises the isolated decoder and synthetic
transcription broker. Start this variant without real provider credentials:

```sh
docker compose -f compose.yaml -f compose.qa.yaml -f compose.av.yaml -f compose.av.qa.yaml up -d --build --wait
```

The decoder has no network or secrets. The broker uses only saved transcripts
for exact synthetic fixtures; unknown recordings fail honestly. Port 8097 is
loopback-only for worker checks and the native Codex bot. The fake Telegram UI
supports Audio, Voice, Video and Video note uploads and playback controls.
See [fixtures and real-model setup](testdata/media/README.md).

### Manual acceptance

1. Choose Alice, click `/start`, then select a service by button.
2. Type `помоги закончить`. The agent sees the existing draft and manual history.
3. Click `Подтвердить`. The original Telegram card becomes `booked`.
4. Switch to Boris and try the same one-seat massage: confirmation is rejected.
5. Cancel Alice's booking. Boris can now confirm his existing draft.
6. Choose Visitor. Both buttons and `выбери массаж` are denied by the same API rule.
7. Inject a 429 or deleted-message failure, then change state. The bot retries or
   replaces the deleted card. Business operations are not repeated.
8. `docker compose restart bot fake`: refresh the browser. Messages, workflow,
   history, model proposals and polling offset persist in PostgreSQL.

The scripted model supports `выбери массаж`, `выбери трансфер` and questions about
the current workflow. It is a deterministic test double, not an LLM. Its typed
interface already receives scoped history and current API state. A model may
propose a booking draft or create/edit an order's extras. Booking confirmation,
booking cancellation and payment decisions require manual actions. The model
cannot set identity, choose a chat, execute arbitrary HTTP, or bypass API rules.

### Orders acceptance

Open `/orders` as Alice. Create an order, add a transfer with its button, then
send `добавь препати в заказ`. The agent edits the same order and its message;
the authoritative total becomes 100 BYN. Choose cash payment to Boris. Switch
to Boris, open `/orders`, and accept or reject the payment. Alice's existing
card updates automatically. Visitor cannot create orders through buttons or the
agent. Old callbacks cannot overwrite a newer order version.

With several editable orders, name the full order ID in an agent edit, for
example `add preparty to order <ID>`. The agent receives compact recent order
summaries and explicitly named orders; full choices are loaded from the API for
version-bound edits. This preserves meals and customer fields without putting
every complete order into the model context. Ask `Что я только что убрал?` after
a manual or agent service removal to exercise shared committed history.

`Питание и ФИО` opens the meal editor inside the fake Telegram. Enter names,
select meal quantities, press Calculate and Save. Quotes show server-calculated
containers and utensils. RU/EN labels are available. Paid/proof orders and closed
events are read-only. A stale save preserves the form until explicit reload;
retrying a lost save response reuses the operation key. Chat cards refresh and
agent extra edits preserve meals/customer data.

The editor is served by the bot's HTTP gateway and delegates business operations
to Core API. Every editor API request verifies signed Telegram `initData` (one
hour lifetime, 30-second future-clock allowance). The browser reads the standard
`tgWebAppData` launch fragment; no external JavaScript or debug user-ID bypass
is required. The fake Telegram issues signed launches from actual Web App
buttons and proxies the same editor. Its plain-HTTP loopback URL is a sandbox
fixture; real Telegram deployment requires a public HTTPS Mini App URL.

Proof attachment UI, notifications, reminders, exports and full production parity
are still pending. The fake chat displays sent user messages as well as bot
messages and editable keyboards.

### Direct API stand

For independent HTTP QA, expose the authenticated sandbox API on loopback:

```sh
docker compose -f compose.yaml -f compose.qa.yaml up -d --build --wait
```

The Telegram UI remains on port 8090; Core API is on `http://127.0.0.1:8091`.
This uses the same disposable fixture data. Only one QA writer may mutate it.
QA may use additional Compose projects for independent data and alternate ports.

The fixture authentication contract is `Authorization: Bearer <body>.<signature>`.
Body is unpadded base64url JSON with `sub` (`alice`, `bob`, `visitor`),
`actor: "sandbox-bot"`, `aud: "zns-core"`, and `exp` (Unix seconds, now + 60).
Signature is unpadded base64url HMAC-SHA256 over the encoded body, using the
disposable key `sandbox-only-do-not-use-in-production-123456789`.
This is an isolated test contract, not production Zitadel authentication.

- `GET /v1/order-events/sandbox-festival`: authoritative menu and extra prices.
- `GET /v1/order-events/sandbox-festival/orders?cursor=`: owner-scoped page
  `{orders, next}`; pass `next` as the next cursor until empty.
- `GET /v1/order-events/sandbox-festival/payment-inbox?cursor=`: admin-only pages.
- `POST /v1/order-events/sandbox-festival/quote`: a choice; prices are recomputed.
- `POST /v1/order-actions`: `{event_id, name, order_id?, version, attempt?, key,
origin, choice?, payment_admin?, proof_file?, country?}`. Mutations bind the
  current version; payment decisions also bind the attempt. Reuse the same key
  only to retry the same operation. `origin` is `manual` or `agent`.
- `GET /v1/order-events/sandbox-festival/history`: last 30 committed changes to
  the caller's orders, oldest first. Each includes action, origin, version,
  timestamp, previous/current extras and resulting total. Deleted orders retain
  history. Failed actions and retries add no records. Older audit rows without
  snapshots are omitted; unavailable historical selections are not inferred.
- `GET /v1/order-events/sandbox-festival/orders/{id}`: complete owned order or 404.

Payment documents: select **Отправить чек** on an order, then send a document
through the stand file input. Selection survives restart and binds the order
version. A stale upload is rejected; select the current order again. Documents
never invoke the agent. Choose a country/admin after upload; the payment inbox
has **Открыть чек**, accept and reject. Cancellation unlocks the order. The
current fixture has Boris as the BE payment administrator.

- `POST /v1/order-proofs?filename=receipt.pdf`: raw bytes, 1–20 MiB, authenticated
  bookable owner. Returns `{id, filename}`. Identical owner/name/bytes reuse an ID.
- `proof_file` in a `proof` action must be an uploaded ID owned by that user;
  arbitrary Telegram file IDs and foreign proof IDs are rejected.
- `GET /v1/order-events/{event}/orders/{id}/proof`: proof metadata for owner or
  event payment admin while the order is proof/paid; otherwise 404.
- The same path plus `/file` downloads original bytes as an attachment.
- `POST /lab/document?user=101&filename=receipt.pdf`, `X-Sandbox: 1`, raw bytes:
  fake Telegram upload. The document appears in chat and a real-format update.
  `getFile`, private file download, `sendDocument` and `forwardMessage` are supported by the fake.

File bytes live in PostgreSQL. Telegram review sends the immutable stored bytes,
including API-uploaded files; changing or deleting the original message cannot
replace the accepted receipt. Files remain downloadable through the Core API.
Document delivery follows Telegram semantics (a transport failure
can cause a duplicate message on retry); the order mutation is idempotent.
Payment/capacity notices now arrive without opening `/orders`. Core enqueues
them in the order transaction; the bot uses a separate delivery identity and
refreshes current cards before sending the notice. Ordinary failures retry after
five seconds; blocked recipients are recorded as failed and do not block others.
Delivery receipts avoid another send when only the Core acknowledgment fails.
A crash after Telegram accepted a send but before its receipt was persisted may
still produce a duplicate message: Telegram has no send idempotency key.

Unpaid/cash orders with a positive total receive one overdue reminder (default
48 hours, `ORDER_REMINDER_AFTER` on the API). The API scans on startup and every
minute. Eligibility is rechecked before delivery. As in Python, reminders are
claimed before attempting delivery and are not repeated after an ambiguous
failure. Notifications are included in the recipient's agent interaction history.

Order XLSX export: `/exportfoodorders`, the administrator's `📥 XLSX` button, or
an explicit agent request such as `Экспорт заказов в xlsx`. All three use the
requesting user's authorization; only current event payment administrators with
access may download `GET /v1/order-events/{event}/export`. The agent receives no
workbook contents. The five sheets preserve historical prices and distinguish
proof/paid aggregates from pending cash and unpaid orders. Dates are UTC ISO
timestamps; catalog columns are sorted by stable key. Headers are frozen and
filtered; formula-like customer text stays literal text. Deleted orders are absent.
Export limits: 10,000 orders, 16 MiB serialized choices, 1,000,000 cells, 20 MiB
compressed workbook, 32,767 UTF-16 units per text cell. Exceeding a limit returns `413 export_too_large`, never a
truncated report. A repeated completed Telegram update reuses its delivery receipt;
the same documented send-before-receipt crash window applies to file delivery.

The delivery fixture token has `sub: "telegram-delivery"`, `actor: "sandbox-bot"`,
`aud: "zns-notifications"`, a short expiry and the same test HMAC signature
algorithm. User tokens cannot access these routes; delivery tokens cannot call
the user business API:

- `GET /internal/notifications`: at most 25 pending records, FIFO per recipient.
- `POST /internal/notifications/{id}/complete`: `{failure: ""}` for success;
  `telegram_retry`, `telegram_forbidden`, `telegram_rejected`, `reminder_failed`
  report transport outcomes. Retries do not replay completed records.
- `POST /internal/notifications/{id}/claim-reminder`: returns `{claimed}` and
  permits at most one reminder delivery attempt.

Sandbox controls for independent QA (only the current `sandbox-festival`):

```powershell
# Age exactly one QA-owned order and run a due-reminder scan.
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_ID=YOUR_ORDER_ID -e ORDER_FIXTURE_AGE=72h migrate fixture
# Close the event, then restore its normal deadline after the test.
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_DEADLINE=2020-01-01T00:00:00Z migrate fixture
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_DEADLINE=2030-09-24T21:00:00Z migrate fixture
# Switch Boris to RU payments, then restore BE after the test.
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_ADMIN_COUNTRY=ru migrate fixture
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_ADMIN_COUNTRY=be migrate fixture

# Leave one seat above existing proof/paid reservations, without changing orders.
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_EXTRA=excursion_grodno_overview -e ORDER_FIXTURE_REMAINING=1 migrate fixture
# Restore the published capacity after the scenario.
docker --context desktop-linux compose run --rm --no-deps -e ORDER_FIXTURE_EXTRA=excursion_grodno_overview -e ORDER_FIXTURE_REMAINING=default migrate fixture
```

Run these from `platform`; omit `--context desktop-linux` on Linux. No API or bot
role can change fixtures. The CLI uses the isolated schema-owner service and
refuses to run outside `ZNS_ENV=sandbox`. Coordinate global deadline/country/capacity
changes with the stand writer. Capacity controls accept 1–1000 remaining seats;
reset refuses to reduce capacity below existing reservations. Cancel excess
QA-owned proofs before restoring the default; preserve other users' reservations.
Age only your own synthetic orders. To simulate a
blocked chat, `POST /lab/blocked` with `X-Sandbox: 1` and
`{user: 101, blocked: true}`; restore with `false`. Block state survives restart.

Independent export QA also has a schema-owner `export-fixture` CLI (sandbox only).
Choose exactly one action per invocation. These controls are synthetic fixture
setup, not user actions or import functionality:

```powershell
# Replace only your own unpaid order's saved Choice JSON. Set the environment
# variable from your harness to avoid shell quoting. Keep the live catalog unchanged.
$env:EXPORT_FIXTURE_CHOICE='{"customer":"Historical QA","days":{"friday":{"mealtimes":{"dinner":{"dishes":[{"name":"caesar","count":2,"price":1.23,"total":2.46},{"name":"retired-dish","count":1,"price":0.50,"total":0.50}],"service":{"items":[],"total":0},"total":2.96}},"total":2.96}},"extras":{},"total":2.96}'
docker --context desktop-linux compose run --rm --no-deps -e EXPORT_FIXTURE_ORDER_ID=YOUR_ORDER_ID -e EXPORT_FIXTURE_CHOICE migrate export-fixture
Remove-Item Env:EXPORT_FIXTURE_CHOICE
# Revoke Boris's main-event administrator membership; restore BE afterwards.
docker --context desktop-linux compose run --rm --no-deps -e EXPORT_FIXTURE_ADMIN_ENABLED=false migrate export-fixture
docker --context desktop-linux compose run --rm --no-deps -e EXPORT_FIXTURE_ADMIN_ENABLED=true migrate export-fixture
# A separate event qa-export-YOURTAG avoids flooding the bot's main-event GUI.
docker --context desktop-linux compose run --rm --no-deps -e EXPORT_FIXTURE_BATCH_TAG=YOURTAG -e EXPORT_FIXTURE_BATCH_COUNT=10001 migrate export-fixture
# GET /v1/order-events/qa-export-YOURTAG/export must fail with export_too_large.
# Cleanup only this tag. Do not perform business actions on these batch orders.
docker --context desktop-linux compose run --rm --no-deps -e EXPORT_FIXTURE_BATCH_TAG=YOURTAG -e EXPORT_FIXTURE_BATCH_COUNT=0 migrate export-fixture
```

Tags are 1–40 lowercase ASCII letters/digits/hyphens. Counts are 0–10,001;
zero removes the tagged fixture, and repeated cleanup is harmless. Creation
refuses an existing tag. Cleanup refuses orders changed through business actions.
Saved Choice input is at most 256 KiB; it increments the order version and is
refused once payment is locked. Use only synthetic data and QA-owned order IDs.
Coordinate administrator changes with the exclusive stand writer. Restore
membership and remove your batch before releasing the stand.

Mini App public contract uses `Authorization: tma <signed initData>`:

- `GET /miniapp/api/orders/{id}` returns `{order,event,editable}`.
- `POST /miniapp/api/quote` accepts `choice` and returns canonical totals.
- `POST /miniapp/api/orders/{id}` accepts `{version,key,choice}`. Identity, event,
  operation and manual origin are server-derived; client overrides are rejected.
- `POST /lab/webapp` with `X-Sandbox: 1` and `{user,message_id,url}` opens only a
  matching Web App button in that fixture user's chat, returning a launch URL.

History also includes dish quantity deltas (`day`, `meal`, `name`, `before`,
`after`) and names of changed customer fields. It does not invent their values.

Use only synthetic test data. A QA agent may build its own harness against these
public contracts without reading the implementation.

`choice` accepts optional `customer`, `customer_first_name`, `customer_last_name`
and `customer_patronymus` strings (up to 1,024 UTF-8 bytes each), `days`, `extras`
and `total`. Meal structure:

```json
{
  "customer_first_name": "Alice",
  "customer_last_name": "Example",
  "days": {
    "friday": {
      "mealtimes": {
        "dinner": { "dishes": [{ "name": "caesar", "count": 2 }] }
      }
    }
  },
  "extras": { "preparty": 0 }
}
```

The event response contains `menu.choices[day][meal][category]` dish IDs,
`menu.dishes[id]` metadata and `menu.service_items`. Dish counts are integers
from 1 to 10,000. Omit empty days/meals; null objects are invalid. Client price,
total and service values never set authoritative prices. Quote and saved order
responses include canonical `price`, `count`, `total`, `service.items` and
`service.total`; utensils count once per meal, containers follow dish quantity.

### OpenAI mode

The real agent uses **OpenAI `gpt-6-luna`**, via the Responses API and strict
structured output. The model service defaults to OpenAI; the base Compose file
explicitly substitutes the scripted fixture. To use the real model, set
`OPENAI_API_KEY` in your shell and run:

```sh
docker compose -f compose.yaml -f compose.openai.yaml up -d --build --wait
```

This optional configuration gives only the model container outbound access and
the key. It sends the sandbox user's conversation context to OpenAI and incurs API
usage. Keys and API user tokens are never placed in prompts or logs. Requests use
`store: false`, a bounded input context and a 1,600-token output cap. Refusal,
incomplete/invalid output, timeout or rate limits leave manual buttons available.
No silent fallback to another model is allowed. The adapter's HTTP contract is
tested with fixtures; a live paid OpenAI call is not part of deterministic CI.

Implementation references: [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)
and [Structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs).

`docker compose stop` preserves data. `docker compose down` removes containers and
network but retains the named PostgreSQL volume. To start a separate clean fixture,
use `docker compose -p zns-sandbox-other ...` and adjust occupied host ports.

## Tests

Go 1.27+ is required. Tests create a separate randomly named database per test and
drop only that database on cleanup. `TEST_DATABASE_URL` must point to a disposable
cluster with CREATEDB privilege. It must never point to production.

```sh
export TEST_DATABASE_URL='postgres://postgres:sandbox-owner-only@127.0.0.1:55432/zns?sslmode=disable'
go test -race -count=1 -coverpkg=./internal/... -coverprofile=coverage.out ./cmd/... ./internal/... ./integration/...
go vet ./cmd/... ./internal/... ./integration/...
go mod verify
SANDBOX_URL=http://127.0.0.1:8090 go test ./integration -run TestLiveSandbox -count=1
```

PowerShell: use `$env:TEST_DATABASE_URL='...'` and `$env:SANDBOX_URL='...'`.
Windows race tests require a C compiler; CI runs them on Linux. Without a test
database, integration tests explicitly skip locally and fail in CI. No in-memory
repository replaces PostgreSQL in the integration suite.

Browser checks are also reproducible (Node 22+): `npm ci`,
`npx playwright install chromium`, then `npm run test:browser` against the default
scripted sandbox. `BROWSER_CHANNEL=msedge` uses an installed Edge instead. The suite
checks mouse and emulated-touch interaction separately and saves screenshots in
`test-results/`. It is not a hardware touch-device test.

## Boundaries and delivery guarantees

- `internal/core` and `internal/orders`: business transactions, access rules,
  capacity, prices, deadlines, version checks, audit and idempotency. Only the
  API's business services write the Core schema.
- `internal/api`: HTTP authentication and strict typed DTOs.
- `internal/bot`: direct manual calls and agent calls share API execution and one
  Telegram renderer. A database lock allows one poller/renderer replica.
- `internal/agent`: model port and typed view/action proposals; hostile fixtures.
- `internal/telegram`: actual Bot API JSON transport contract.
- `internal/sandbox`: manual chat, fake Bot API, persisted fake state and faults.
- `core` and `bot` database schemas have separate runtime roles. Neither runtime
  uses the schema owner's credentials. Identity fixture signing is sandbox-only;
  production Zitadel authentication is a later gate, not a fallback.

API operations commit state, operation receipt, audit and outbox in one transaction.
At stage 1 the bot reconciles its small set of active cards by polling current API
state; consuming outbox notifications is a later scaling step. Callbacks always
check the user's current access and workflow version. Stored agent proposals bind
the original version across retries. The model does not issue human confirmation.

Telegram delivery is at least once: Telegram has no general sendMessage idempotency
key. A crash after sending a new message but before saving its ID can duplicate a
card; the workflow is still idempotent and obsolete buttons fail server checks.
Periodic edit failure handling restores a deleted card when its view changes.
The generic booking fixture catalog is immutable; real massage schedules and
price/version binding remain later stages. The booking fixture has one current
workflow per user; festival ordering supports multiple owned orders.

Dependabot opens weekly Go, npm, Docker and Actions PRs. CI runs real PostgreSQL tests,
race detection, fuzzing, formatting, vet, dependency verification and container
smoke tests. Required checks and auto-merge rules must be configured on GitHub
before automatic merging is enabled. No deployment workflow is changed.
