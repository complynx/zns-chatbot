# Administrative broadcast parity

Status: implemented slice under builder validation; independent Code QA and Telegram-like Functional QA are pending. This is not complete migration acceptance. No production or real Telegram sends are used during validation.

## User flow

`/send_message_to <recipients> --msg "text"`, `--html`, and legacy `--md` create a durable preview. Independent `--template` selects the Go template language described below. `--forward`, missing content, or empty explicit content requests an attachment. Telegram CODE/PRE spans preserve argument boundaries, including Unicode and embedded quotes.

Reply explicitly to the stored prompt within ten minutes, or ask the assistant to use the current message for that pending input. Other messages/media remain ordinary conversation. Forwarding uses the received message's chat and message IDs, not its forwarded origin. Prompt cancellation, the legacy cancel marker and durable expiry are supported. A restart does not restore expired input authority. A crash between sending and registering a prompt can leave an inert orphan prompt; replay creates an authoritative prompt.

Every preview has a complete deduplicated audience, exact count, pages of 20 recipients and access to each recipient's full content. There is no 1000-recipient cap. Large explicit command bodies retain the HTTP request budget; pass/selector audiences are not silently truncated. Clicking Send is always a separate manual confirmation. The assistant can create a preview, inspect pending hints, attach its host-bound current message or cancel input; it cannot send. Current global administrator role is checked at discovery/help/bindings and every domain call. The current VM retains its initial binding universe; grants take effect next run and revocations immediately remove access.

Request-key replay loads the prior snapshot before re-resolving audience, reading names or calling a provider. New snapshots use repeatable-read transactions with bounded serialization retries. Per-recipient render jobs freeze the profile snapshot before model calls. The provider runs outside database transactions; completed content/error survives restart and is never rerendered at delivery. Failed renders are visible and block enqueue of the entire draft. Create a new draft after correcting an error. Cancellation stops pending work; already in-flight sends cannot be recalled. Existing delivery fencing and rate-limit policy are unchanged. Neither notifications nor Telegram delivery promise exactly-once behavior across a crash after upstream success.

## Go template language: explicit migration decision

The user accepted a more Go-native template language. Standard `text/template` handles plain text and legacy Markdown; `html/template` handles HTML. No Gonja dependency, Tornado adapter, Python runtime or custom expression interpreter is installed. Python/Tornado syntax is intentionally replaced; it is not claimed compatible. Plain/Markdown interpolation is literal, while HTML interpolation uses Go contextual escaping. This differs from Tornado's default global expression escaping.

Examples:

```text
/send_message_to 101 --template --msg 'Hello {{.first_name}}'
/send_message_to 101 --template --html 'Hello {{.user_name}}: {{template "user_link" .}}'
/send_message_to 101 --template --msg '{{if .first_name}}{{.first_name}}{{else}}Hello{{end}}'
```

`user_name` follows source name precedence: locale-specific inner name, English inner name, print name, first+last, username, numeric ID. `user_informal_name` preserves a present cached source value, including null/empty. When missing, the existing provider uses authorized name fields and the source naming algorithm; its result is frozen before confirmation. Nonempty generated values are cached conditionally without overwriting a concurrent explicit edit. An empty/unknown model result is frozen for this draft without globally poisoning the cache. Provider absence/error is an explicit per-recipient failure, never first-name substitution.

`.user_link` is an ordinary string and is escaped by HTML mode. The fixed named template `{{template "user_link" .}}` emits a safe link using literal `https://t.me/` or `tg://user?id=` prefixes, escaped components and a trusted numeric ID. No arbitrary profile value is marked safe HTML. Templates cannot redefine the named link template or recursively call other templates.

Missing keys fail. Comparisons/boolean operations, len/index/slice and ordinary conditionals are allowed; arbitrary function calls, formatting helpers with unbounded allocation, imports and host methods are not exposed. A collection loop must range over a data field, not an integer/expression; one range nesting level, at most 128 elements per collection, data depth 8, syntax depth 16, weighted work budget 100000 and output 16384 bytes apply. Final content still passes the existing 4096 UTF-16-unit send validation. Exceeding a budget fails the recipient; output is never truncated into a send. Profile/template input budgets are operational errors, not audience caps.

## Selectors and profile fields

The user accepted a few basic filters, with richer selection performed by the agent over authorized data. JSON-object selectors operate on a private same-deployment field-preserving projection. The command's top-level bot_id is replaced by deployment scope. Ordinary users cannot access the projection. Missing, null, empty, false, zero and exact JSON numbers remain distinct. Current writes overlay source fields only when explicitly written; defaulted typed columns do not invent source presence. Import provenance is retained separately from runtime overrides.

Supported grammar: implicit AND, $and/$or/$nor, equality, $eq/$ne/$in/$nin/$exists/$gt/$gte/$lt/$lte. Scalar equality also matches array members; JSON numbers are compared without float rounding. Supported fields are the authorized source profile fields, including user ID, names, locale names, role, legal name/passport/frozen state and practitioner data. Unknown fields/operators fail before snapshot. `_id` is not fabricated from a Go owner/source hash. Nested paths, PCRE expressions, executable predicates, geospatial and arbitrary MongoDB operators are not silently translated. This typed grammar does not constitute unrestricted Mongo find compatibility; its migration boundary remains explicit in the overall parity tracker.

Source fields and current overrides live in core.admin_broadcast_profiles, owned by the separate projection/import slice. No profile row means selector resolution fails explicitly instead of returning a deceptively empty audience. Template topics and missing profiles produce visible render failures.

Read/render projections add the trusted Telegram user ID and configured deployment bot ID. The latter uses the existing validated deployment namespace; imported `bot_id` never supplies authority. If deployment identity is unavailable, `bot_id` is absent and strict templates fail explicitly instead of trusting imported data. This overlay does not modify immutable source evidence.

The administrator-only `broadcasts.audience` tool enumerates all local users using keyset pages of at most 20 items and a 24 KiB response budget. Each item includes a user ID, a bounded display-name excerpt and available field names. Follow `next_cursor` while `more` is true. `broadcasts.profile` returns complete JSON chunks for a selected user; concatenate their `json` strings before parsing. Its cursor binds actor, operation and user; a changed profile yields `read_stale`, requiring a fresh read. Profiles have a 1 MiB read budget; larger values fail explicitly. The agent can apply JavaScript filters and pass explicit recipient IDs to `broadcasts.preview`. These tools expose no SQL and cannot enqueue delivery. Discovery and calls require current global broadcast authority; ordinary actors see neither tool name nor description.

## API and worker boundary

### Sandbox configuration and credentials

This interface section describes candidate27. It is a public operator contract, not a request to inspect implementation. General configuration syntax is described in [configuration.md](configuration.md); script execution is described in [agent-scripting.md](agent-scripting.md) and [script-worker.md](script-worker.md). Those documents also describe other stages; this section defines the broadcast settings needed here.

| Setting | Purpose |
| --- | --- |
| `ZNS_ENV=sandbox` | Select the local sandbox identity adapter. |
| `ZNS_DATABASE__URL` | Isolated PostgreSQL database, migrated through 059. |
| `ZNS_AUTH__SIGNING_KEY` | Shared API/bot sandbox HMAC key, at least 32 bytes. There is no separate broadcast signing key. |
| `ZNS_CORE__URL` | API origin for a separate `bot` process. Integrated `app` does not need an external Core URL. |
| `ZNS_TELEGRAM__BASE_URL`, `ZNS_TELEGRAM__TOKEN` | Fake Telegram origin and its synthetic token. These are distinct from API bearer credentials. |
| `ZNS_MODEL__PROVIDER=remote`, `ZNS_MODEL__URL` | Local model adapter origin. Configure the API process as well as the bot when they run separately: preview rendering happens at the API. Integrated `app` shares its model configuration. |
| `ZNS_SCRIPT__ENABLED=true`, `ZNS_SCRIPT__SOCKET=/run/script-ipc/evaluate.sock` | Enable Sobek assistant execution on the bot/app using the isolated Unix-socket service. Manual commands do not require Sobek. |
| `ZNS_TELEGRAM__TOKEN=999:synthetic` | In sandbox, a numeric synthetic token prefix supplies the trusted deployment namespace. Use the same token on API and bot and configure the fake endpoint accordingly. A token without a numeric prefix leaves the namespace absent. Do not set `ZNS_AUTH__ZITADEL__BOT_ID` in sandbox: that field requires the complete separate Zitadel authentication mode. Neither setting grants broadcast authority. |

The sandbox signing format is `base64url(JSON claims).base64url(HMAC-SHA256(key, encodedClaims))`, with unpadded URL-safe Base64 and no JWT header. Claims are `{"sub":"<owner>","actor":"sandbox-bot","aud":"zns-core","exp":<Unix seconds>}` for user requests. Use the fixture's owner identifier, not an arbitrary Telegram numeric ID. For service requests, use `sub: "telegram-delivery"` and `aud: "zns-notifications"`. Normal bot credentials last one minute; test clients should mint fresh short-lived credentials. Both use `Authorization: Bearer <token>`. A service token cannot substitute for a user token, and user tokens cannot call service routes. Role membership is checked separately on every broadcast operation; possession of a valid user token does not grant broadcast access.

The remote naming contract is `POST <model-origin>/broadcast-name`, `Content-Type: application/json`, with an object of authorized name fields such as `{"first_name":"Anna"}`. A successful response is exactly `{"text":"Аня"}`: the text is the already normalized informal name. `{"text":""}` represents an unknown name. The local remote adapter adds no bearer header; keep this synthetic endpoint inside the isolated stand. Transport timeout is ten seconds and the response budget is 16 KiB. Non-success status or malformed response becomes a visible render failure. The remote model's normal assistant routes remain necessary for agent conversations. The built-in `scripted` provider does not implement informal-name generation. Other supported naming providers are `openai` (requires `ZNS_MODEL__OPENAI_KEY`) and synthetic-only `codex` (requires an absolute `ZNS_MODEL__CODEX_EXECUTABLE`); independent sandbox validation should use a local fake provider rather than real services. No broadcast-specific model secret or endpoint setting exists.

Build the script service from candidate27's `snapshot/Dockerfile.script`, with `snapshot/` as the build context. Its runtime entrypoint is `scriptservice`, which starts a fresh Sobek helper per request. Share a dedicated IPC volume at `/run/script-ipc`, owned by UID/GID 10002 with directory mode 0770. The service runs as 10002:10002 and creates socket mode 0660. The bot/app runs as 10001; add supplementary group 10002 and mount that same volume read-only into it. The worker requires no network, database, signing key, model key or Telegram token. Use the isolation settings in [script-worker.md](script-worker.md): read-only root, no network, dropped capabilities, no privilege escalation and bounded memory/CPU/PIDs. Merely setting `ZNS_SCRIPT__ENABLED` without a reachable service does not provide agent execution.

### HTTP request schemas

All routes below use `POST`. Send `Content-Type: application/json`. Bodies shown with `{}` have no required fields. IDs, offsets, attempts and timestamps are JSON integers; `user_id`, cursor and key values are strings. Omit `cursor` or use an empty string for the first page. Each `key` is a nonempty caller-chosen idempotency key, at most 200 bytes; reuse it only for the same operation content. Commands include the `/send_message_to` prefix. User routes are owner-scoped and require current global broadcast authority except capability discovery, which returns the current boolean.

| User route | Request body | Successful response |
| --- | --- | --- |
| `/v1/admin-messages/capabilities` | `{}` | `{"allowed":true}` or `false`. |
| `/v1/admin-messages/preview` | `{"key":"preview-1","command":"/send_message_to 101 --msg 'Hello'"}` | Message object. Use input/start for a command awaiting content. |
| `/v1/admin-messages/input/start` | `{"key":"input-1","command":"/send_message_to 101","chat_id":101}` | Input object. |
| `/v1/admin-messages/input/prompt` | `{"id":1,"chat_id":101,"prompt_id":20}` | `{"ok":true}`; bind the actual sent prompt. |
| `/v1/admin-messages/input/pending` | `{"chat_id":101}` | Array of pending Input objects. |
| `/v1/admin-messages/input/attach` | `{"input_id":1,"chat_id":101,"prompt_id":20,"key":"source-1"}` | Message object; key must reference an existing transport-registered source for this actor/chat. |
| `/v1/admin-messages/input/cancel` | `{"id":1}` | `{"ok":true}`. |
| `/v1/admin-messages/review` | `{"id":1,"offset":0}` | `{"id":1,"state":"preview","total":1,"offset":0,"more":false,"items":[...]}`. Advance offset by returned item count while more is true. |
| `/v1/admin-messages/{id}/resume` | `{}` | Message object after resuming preparation. |
| `/v1/admin-messages/{id}/send` | `{}` | `{"ok":true}` after explicit human confirmation. Not exposed as an assistant tool. |
| `/v1/admin-messages/{id}/cancel` | `{}` | `{"ok":true}`. |
| `/v1/admin-messages/{id}/results` | `{}` | Array of delivery result objects; paged review is preferred for complete large-audience navigation. |
| `/v1/admin-messages/audience` | `{"cursor":""}` | `{"items":[{"user_id":"101","name":"Anna","fields":["first_name"]}],"more":false,"next_cursor":""}`. |
| `/v1/admin-messages/profile` | `{"user_id":"101","cursor":""}` | `{"json":"{\"first_name\":\"Anna\"}","more":false,"next_cursor":""}`. Concatenate json strings across pages before parsing. |

A Message is `{"id":1,"state":"preview","request":{"destinations":[{"chat":"101"}],"content":{"text":"Hello"}}}`. State can also be preparing, queued or cancelled. The request is descriptive; each recipient's frozen content and failure are authoritative in review. A delivery object has `id`, `message_id` (draft ID), `destination`, `content`, `state`, `attempt`, `telegram_message_id` and `failure`. Destination is `{"chat":"101","thread":42}` with optional thread. Content uses `text` and optional `parse_mode` (`HTML` or `Markdown`), or `from_chat` and `from_message` for forwarding. An Input contains `id`, `chat_id`, `prompt_id`, `state`, `forward` and RFC3339 `expires_at`.

| Service route | Request body | Successful response |
| --- | --- | --- |
| `/internal/admin-messages/source` | `{"actor":"<owner>","key":"source-1","chat_id":101,"message_id":21,"html":"Hello"}` | `{"ok":true}`. Register only an actual received message; html is its Telegram-entity-preserving representation. |
| `/internal/admin-messages/claim` | `{}` | `{"found":true,"delivery":{...}}`; false means no eligible delivery. |
| `/internal/admin-messages/complete` | `{"id":1,"attempt":1,"message_id":90,"failure":"","retry":false,"retry_after":0}` | `{"ok":true}`. Here message_id is the returned Telegram message ID, not the draft ID; id and attempt come from claim. retry_after is seconds. |
| `/internal/admin-messages/input-expiry/claim` | `{}` | `{"found":true,"expiry":{"id":1,"chat_id":101,"attempt":1,"language":"en"}}`; false means no eligible notice. |
| `/internal/admin-messages/input-expiry/complete` | `{"id":1,"attempt":1}` | `{"ok":true}`; use the claimed expiry ID and attempt. |

Source registration is a host boundary, not an assistant tool. A selected source expires after ten minutes. A source request cannot change an existing actor/key binding. For delivery failures use the finite worker failure vocabulary rather than raw Telegram errors; retries respect the server cooldown and attempt fence. HTTP errors return a finite `code` field, for example unauthorized, forbidden, invalid_json or idempotency_conflict. Failed requests do not grant fallback authority.

### Assistant request schemas

These are the advertised business-tool arguments within Sobek Execute; the host supplies actor/chat/source context and durable request keys. They are not arbitrary HTTP payloads.

| Tool | Arguments |
| --- | --- |
| `broadcasts.preview` | `{"command":"/send_message_to 101 --msg 'Hello'"}`; command max 16384 characters. |
| `broadcasts.pending` | `{}`. |
| `broadcasts.attach` | `{"input_id":1}`; uses the current received message. |
| `broadcasts.cancel` | `{"input_id":1}`. |
| `broadcasts.audience` | `{"cursor":""}`. |
| `broadcasts.profile` | `{"user_id":"101","cursor":""}`. |

Tool schemas reject additional properties. Use runtime discovery for the available names and descriptors. There is no broadcasts.send binding.

User-authenticated endpoints under /v1/admin-messages provide command preview/resume, input start/prompt/pending/attach/cancel, current capability, paged review and authorized audience/profile reads. The bot registers the selected current message with a separate service credential before attachment. Attachment accepts only the input/source key and prompt context; user/model-supplied content or forwarding IDs cannot manufacture a trusted source. Source records expire after ten minutes. Claim/completion and timeout notices remain behind that service credential. No public claim/complete endpoint was added. The renderer stores finite failure codes; diagnostics never include commands, profiles, names, messages or raw provider errors.

Migration059 introduces input lifecycle, source projection, immutable recipient content/profile snapshots and render leases. Existing drafts are backfilled from their literal request; existing delivery content falls back safely to the old literal field. Root owns rollout and progress; this document records the contract rather than granting acceptance.

Runtime rollback requires stopping broadcast ingress first. Older workers read the draft's source text and cannot deliver new per-recipient templates correctly. Drain or explicitly cancel new-format pending work with the current worker before starting an older broadcast worker; preserve its records and results. Reverting code does not require dropping059 tables or deleting source projections. Literal drafts created before059 remain compatible through the backfill/fallback. This is an additive data migration, not unrestricted mixed-version worker compatibility.

## Sobek campaign review and continuation

`broadcasts.review({id, offset?, cursor?})` returns complete JSON chunks of one owner-authorized review page. Concatenate `json` while outer `more` is true using `next_cursor` and the same ID/offset, then parse. Advance the page offset by returned `items.length` while page `more` is true. Cursors bind actor, campaign, page and content digest; changed data requires restarting the page. Keep subsequent script evidence concise within normal result/call budgets.

`broadcasts.show({id, offset?})` displays native review controls in the host-bound chat. It may resume unfinished preparation but never enqueues delivery. Only explicit native Send confirmation can do that. Current global broadcast rights and campaign ownership apply to discovery, help and execution. Revoked cached calls are denied. Interrupted display remains uncertain, and exact incoming-update replay does not automatically resend it. Receipt metadata does not promise exactly-once Telegram delivery.

Both bindings passed independent scoped Code QA and rendered EN/RU mouse/touch Functional QA before integration. Final whole-product acceptance remains separate.
