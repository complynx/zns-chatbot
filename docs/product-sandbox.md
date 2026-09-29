# Product acceptance fixtures

`compose.product.yaml` defines the combined acceptance topology: one business
`app` process, Fake Telegram, PostgreSQL and isolated JS/AV/sticker helpers.
The model is deterministic; ASR uses synthetic fixtures. It neither reads a
real API key nor contacts OpenAI. This topology is being validated and is not
yet an accepted full-product release.

From the repository root:

```sh
docker compose -f compose.product.yaml up -d --build
docker compose -f compose.product.yaml ps
docker compose -f compose.product.yaml stop app
docker compose -f compose.product.yaml up -d --no-deps app
docker compose -f compose.product.yaml stop
```

Fake Telegram is `http://127.0.0.1:8118`; the same-process API/Mini App is
`http://127.0.0.1:8117`. PostgreSQL is on loopback port 55434 with a separate
`zns-product` volume. The main app uses a non-superuser account for the two
business schemas; migrations/explicit fixtures use the synthetic owner account.
Never point this configuration at production. Stop the old app before starting
its replacement. A normal stop preserves database evidence; no command above
deletes the volume.

The checked-in `platform/sandbox/product.yaml` exercises YAML configuration with
environment overrides. Helper decoders have no network. Script IPC is shared
through an explicit group between two distinct nonroot UIDs. AV brokers can
reach only the internal synthetic ASR endpoint; no decoder receives its token.

On an isolated sandbox database, run `zns migrate`, then `zns product-fixture`.
The second command is explicit; ordinary migrations and integration tests do not
change their seed behavior. It requires the original Alice/101, Bob/202 and
Visitor/303 identities. Runtime configuration must use `env: sandbox`.

The built-in sandbox identity adapter recognizes only Telegram IDs101,202,303
with owners `alice`, `bob`, `visitor`. The bundled Fake Telegram input UI uses
the same identities. A custom fake transport alone does not extend that adapter.
For imported-data tests in this mode, prepare reviewed mappings to those exact
synthetic identities. Testing arbitrary imported owners requires a separately
configured Zitadel stand with its own verified mappings.

`POST /lab/input` accepts at most 5,000 UTF-8 bytes of text, not 5,000
characters. Callback data and language codes are each limited to 64 bytes;
the HTTP JSON envelope is limited to 64 KiB. Use an explicit synthetic import
or reviewed database fixture for large historical bodies, rather than treating
this interactive input endpoint as an archive importer.

In sandbox mode, the numeric prefix of `TELEGRAM_TOKEN` supplies the trusted bot
namespace, for example `77:synthetic-test-only`. Do not configure `auth.zitadel`
fields in this mode. Identity revocation in Zitadel must be tested with the
Zitadel adapter, not inferred from sandbox behavior.

The fixture creates:

- An active `sandbox-festival` with a 150 RUB pass tier and Bob as booking/payment
  administrator; a finished `sandbox-past` for knowledge precedence tests.
- A separate active `sandbox-passport-pair` event, **Passport and partner practice**
  / **Паспорт и пара: учебное событие**, requiring passport details. It starts with
  no bookings and two open positive-price tiers (100 places each, 150/200 RUB).
  Bob receives payments. Use Alice and Bob here for the passport and pair lifecycle;
  their bookings in `sandbox-festival` do not occupy this event.
- General, future and past knowledge. Dress code is blue for the upcoming event
  and red for the historical event. Bob can curate and review all three scopes.
  Alice and Visitor cannot review; private memos start empty.
- A massage party starting at the next hour and ending eight hours later, one
  shared table, and Bob as the synthetic specialist.

Application is atomic and recorded once in `public.zns_sandbox_fixtures`.
Repeated calls do not restore revoked permissions or overwrite facts, dates,
preferences or bookings. Start a new disposable database for a fresh acceptance
run; there is no implicit reset. The massage schedule eventually expires.

The passport/pair scenario has its own `product-passport-v1` marker. A deliberate
`zns product-fixture` invocation against an older synthetic database adds this
scenario once without replaying `product-v1`. A pre-existing event with the same
ID is left entirely untouched. Later calls do not reopen expired events, recreate
deleted tiers or restore Bob's revoked payment role. User profiles are shared across
events and are never cleared: existing legal names, passports and frozen state
remain in force. Fresh profile-entry acceptance needs a fresh disposable database.
Only the stand owner may apply this upgrade; do not mutate a frozen FQA stand.

# Local trace collector

Run `go run ./cmd/telemetrylab` from `platform`. It listens only on
`127.0.0.1:8116`. Configure the tested app with `otel.enabled: true`,
`otel.endpoint: http://127.0.0.1:8116`, and `otel.sample_ratio: 1`.

- `GET /lab/traces`: exact OTLP batches as JSON and total received count.
- `POST /lab/failure`, header `X-Sandbox: 1`, body `{"enabled":true}`: fail
  subsequent exports with HTTP 503. Set false to restore collection.

Capture is bounded to 64 batches and 4 MiB; each wire batch is at most 256 KiB.
Use synthetic data only. It retains evidence as received, without redaction or
forwarding. This lets QA check whether the app itself leaks secrets or personal
data. Stop it after the test. A collector failure must not interrupt business
processing; app shutdown still has its configured telemetry flush deadline.

## Independent rendered QA setup

Each reviewer owns a separate Compose project, ports, database and evidence directory. Freeze runtime and helper image IDs before observations. Only one writer changes a stand; do not rebuild its product while QA runs. Another reviewer may receive setup/configuration and public contracts, but not implementation tests or previous findings.

The built-in fake input endpoint is `POST /lab/input` with `Content-Type: application/json` and `X-Sandbox: 1`. Send `user` (101, 202 or 303), `language_code` (`en` or `ru`) and `text`. For a callback, send `data` and the observed `message_id` belonging to that user instead of text. `GET /lab/state?user=101` exposes the synthetic conversation state. Setting a database language alone does not set Telegram's incoming `language_code`; verify both when testing localized identity notices.

Use a separate local Playwright browser for automated rendered checks. A Chromium context with `hasTouch: true` and `isMobile: true`, with actual `locator.tap()` actions, proves emulated touch only. Check mouse separately. Typed command submission through a touch Send button is distinct from a touch picker; report the control actually offered. HTTP observations supplement rendered interactions rather than replacing them.

For transaction races, use an owned proxy or database barrier and observe the blocked request/lock before changing synthetic state. An elapsed delay alone does not prove the intended boundary. Restore fixtures and remove temporary barriers before handing over a stand. After acceptance, delete only that released project's disposable resources and secrets; retain sanitized evidence without database dumps.

### Synthetic Core authentication for independent clients

Only an explicitly configured `auth.mode: sandbox` accepts the local signer. Use the owned stand's `auth.signing_key` (`ZNS_AUTH__SIGNING_KEY`, at least 32 bytes); never use production credentials or save bearer tokens in evidence.

Send `Authorization: Bearer <body>.<signature>`. `body` is unpadded base64url of JSON with `sub` set to the fixture owner (`alice`, `bob` or `visitor`), `actor: "sandbox-bot"`, `aud: "zns-core"`, and `exp` set to a future Unix timestamp in seconds. A one-minute lifetime matches the normal sandbox client. `signature` is unpadded base64url of HMAC-SHA256 over the ASCII body with the signing key. This synthetic wire format is not a JWT and is not accepted by Zitadel authentication.

Authenticated `GET /v1/passes/export` returns XLSX bytes as `passes.xlsx`. Compare a captured Core response with the actual Telegram-like downloaded attachment when verifying exact delivery. A controlled proxy can capture bytes without exposing bearer tokens. Domain authorization still applies to each request; a signed fixture identity grants no payment or export role. Redirect tests must use an owned sink and verify that neither credentials nor request bodies reach it.

### Telegram command menu

Startup holds the exclusive poller lock while replacing the default command button and default/Russian command lists from the current catalog. Failure aborts startup; a restart retries all three idempotent calls. No persistent completion marker can preserve an old menu.

`GET /lab/telegram-menu` returns `menu_button`, `commands` keyed by language (empty key is default), `total_calls`, and up to 64 `recent_calls`. `POST /lab/menu-fault` with `X-Sandbox: 1` and `{"method":"setMyCommands","language_code":"ru"}` injects one matching failure. `setChatMenuButton` with empty language is also supported. Message faults remain independent. The stand implements `getChatMenuButton` and `getMyCommands` for the default scope used by this application.

### Deterministic script plans

Use `model.provider: fixture`, `model.url: http://fake:8080/lab/model` and `ZNS_SYNTHETIC_ONLY=true`. This adapter accepts plans directly; it does not exercise real-provider skill selection or provider instruction filtering.

`POST /lab/model/fixtures` requires `X-Sandbox: 1`. To install and enqueue atomically, omit owner/update_id and send:

```json
{"input":{"user":101,"text":"Inspect my orders","language_code":"en"},"steps":[{"expect":{"text":"Inspect my orders"},"plan":{"view":"orders","text":"","action":null,"script_action":{"code":"return tools.$list();","input_json":"null"}}},{"expect":{"text":"Inspect my orders"},"plan":{"view":"orders","text":"Done","action":null}}]}
```

The response is `201` with `installed:true` and the assigned `update_id`. Alternatively omit input, supply owner (`alice`, `bob` or `visitor`) and a positive update_id, then deliver that Telegram update separately. Do not combine the two scope forms.

Each expectation is a partial planning-input object: maps match subsets, arrays match exactly, and text is required. Limits: 64 KiB per step and 256 KiB installation body; the fake also limits all installed fixtures together to 32 scopes, 32 steps and 256 KiB. Consumed steps still count, so 16 two-step cases can exhaust capacity. Plans still pass normal host/domain authorization. `GET /lab/model/state?owner=alice&update_id=N`, with the same sandbox header, reports next_turn, total, accepted, rejected and last_status. There is no fixture reset/delete endpoint. Restarting the fake clears fixture definitions while restoring persisted update IDs and chat state from its database. Drain pending requests first, or reinstall their needed plans after restart; do not reset the application's Telegram cursor to clear model capacity.

For an owned observation proxy, the adapter sends `POST /lab/model/plan` with planning input JSON and headers `X-Sandbox: 1`, `X-Sandbox-Actor`, `X-Sandbox-Update`, `X-Sandbox-Turn`; the response is a Plan object. Keep captured personal fields and credentials out of published reports. Observing this wire contract does not prove the separate real-provider prompt contract.

### History summaries and retained bodies

The fixture model does not implement semantic history summaries. An uncovered history gap stays explicit and does not prevent planning. For synthetic summary tests, use `model.provider: remote` with an owned local observation service. That service accepts `POST <model.url>/plan` and `POST <model.url>/history-summary`. Summary input is `{"previous":"...","events":[...]}`; return `{"text":"..."}` with nonempty text of at most 2,048 UTF-8 bytes. The input limit is 32 KiB, response envelope limit is 16 KiB, and summary timeout is ten seconds. `history.recent` controls the recent context window; older uncovered eligible events can trigger summarization. These fixtures test integration and concurrency, not real-model summary quality.

The full-history candidate adds public schema migration `066_conversation_message_bodies.sql`. Use the migration supplied with the frozen candidate, not an older live schema. A retained long message has one event with `omitted=false`, a navigation excerpt, and one full body with matching checksum and character count. `omitted=true` means unavailable content, such as a privacy omission; attaching a body must not make that content readable. Discover full-text references from history and inspect `history.read.$help()` for the current cursor contract.

The individual history tool result may contain up to 32 KiB inside Sobek. The final script return has a separate 4 KiB limit. Scripts can inspect complete chunks, return bounded evidence and retain a continuation across turns. Returning an entire large tool response as the final result can correctly fail with `result_limit`. Record complete reads independently from what the script chooses to send to the model. Do not enlarge limits in a QA stand to hide a failure.

For legacy-cache fixtures, capture normal host-created records first. `bot.replies.plan` is an envelope containing `history_generation`, optional `history_redacted`, the model `plan`, and bound domain-command metadata. `bot.interactions` stores `history_reads` as pages with `generation`, and `script_runs` as records with `history_generation` and optional `history_redacted`. Removing a generation field represents the legacy generation-zero format; it does not authorize fabricating a committed domain effect. Keep such mutations confined to an owned synthetic database.

### Public lineup fixture

Configure `lineup.csv`, `lineup.event_year` (1..9998), and an explicit IANA `lineup.timezone`; equivalent environment names are `ZNS_LINEUP__CSV`, `ZNS_LINEUP__EVENT_YEAR`, and `ZNS_LINEUP__TIMEZONE`. The CSV is an operator-owned startup snapshot. Restart to reload it; do not retain it only in importer staging.

```csv
,"Thu, 01.01","Thu, 01.01"
,Main,Side
23:00,DJ A,DJ B
01:00,DJ C,DJ D
```

The first row supplies date labels containing a comma followed by DD.MM; the second names rooms. Remaining rows use HH:MM in column one and DJ labels in room columns. Before 07:00, the set occurs on the next calendar date and belongs to the header's event day. Sets last one hour, excluding their end instant. Invalid/empty time rows are ignored. Ambiguous/nonexistent local DST times reject loading. Limits are 1 MiB, 10,000 rows and 128 columns. No public runtime clock-override setting is provided; use the actual host instant and explicit fixture timezone/date when testing current views.

The candidate `lineup.query` tool takes `scope` (`current`, `day`, `full`) plus optional `date` (YYYY-MM-DD), `room`, `dj`, and `cursor`. Room matching is case-insensitive exact; DJ matching is a case-insensitive substring. Read live help for argument limits. Responses expose `as_of`, `query`, `next_cursor`, `scope`, `status`, `entries`, and `omitted`. Each entry has start, DJ, room and event_date; overlong labels are explicitly marked truncated. Preserve the cursor and filters across bounded continuations. A startup reload invalidates old cursors. This contract does not establish functional acceptance of the candidate.

The typed model Plan uses a stricter lineup wire form than tool arguments. Send `view:"workflow"`, `action:null`, and `lineup_action:{"scope":"full","date":"","room":"","dj":"","cursor":""}`; all five query fields are required, using empty strings for unused filters. Other action fields must be absent. A subsequent script step in the same update shares the four-read lineup budget. To verify persisted UI language independently of the incoming Telegram language code, use an explicit user request with `preferences.setLanguage({language:"en"})` or `ru`, then read `preferences.get({})`.
