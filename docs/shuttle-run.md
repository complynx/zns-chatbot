# Developer workflow

Use the Go toolchain pinned by go.mod. The application keeps typed startup configuration and secrets separate from durable business settings. Copy the example for a private developer configuration and supply the required ZNS_CHATBOT_* secret environment variables. Do not commit the populated configuration.

```sh
cp config/zinusya.example.yaml config/zinusya.yaml
go build -o /tmp/zinusya-avatar ./cmd/zinusya-avatar
ZNS_CHATBOT_AVATAR_EXECUTABLE=/tmp/zinusya-avatar go run ./cmd/shuttle -config config/zinusya.yaml -migrate
```

For an installation with separate database roles, the same command can finish
runtime grants and one-time settings initialization:

```sh
zinusya -config config/zinusya.yaml -migrate -install-runtime-role zinusya_runtime \
  -initial-language ru -initial-parallel-tasks 8
```

The connection must use the migrator/owner credentials. The runtime login role
must already exist and must not have administrative or schema-create privileges.
After app and River migrations, installation grants database/schema access,
current table/sequence privileges and defaults for future objects created by this
migrator. Initial settings are audited once, only at settings version 1 with no
users/tasks. Existing settings and data are preserved on repeat runs. The JSON
receipt contains no credentials. This command starts no runtime, IAM, Telegram,
Loom or provider work. Omitting installation flags retains migration-only behavior.

Apply migrations explicitly before ordinary startup. After the schema is ready, run the same application command without -migrate. The avatar renderer is a separate trusted executable; its absolute path must be available before processing. Startup validates bot/IAM identity and the database binding. One application instance owns its task schema; the instance lease requires at least two task-pool connections.

Webhook management is opt-in through `telegram.webhook_enabled` (default false). When enabled,
`telegram.webhook_url` defaults to `mini_app_origin + /telegram/webhook`; an explicit HTTPS URL
with that path overrides it. Environment overrides are `ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED`
and `ZNS_CHATBOT_TELEGRAM_WEBHOOK_URL`. No request Host header or DNS guess supplies this value.
Start public routing before enabling this mode.

After listeners and workers start, intake/readiness stay closed while startup calls `setWebhook`
with the current secret, `message`, `callback_query`, and
`drop_pending_updates: false`. Each startup reapplies the secret because Telegram does not expose
it through `getWebhookInfo`. Startup checks URL and update types by readback before opening intake.
A rejection, lost response, timeout or mismatched readback fails startup and drains the workers
and listeners. There is one attempt per startup; a process restart reasserts the same desired
configuration. Shutdown does not delete the webhook. Disabled mode performs no webhook calls.

Readiness requires PostgreSQL; IAM or Loom downtime can leave accepted work waiting. A graceful stop preserves task ownership and uncertain external effects for recovery. See the [architecture](shuttle-architecture.md), [schema baseline](shuttle-schema.md) and [deployment identity invariant](shuttle-deployment-scope.md).

An operator can apply a [local Telegram ban](shuttle-user-bans.md) before onboarding.
The same binary records a required actor and reason without starting workers.

## Telegram menu recovery

Only complete menu updates enter the success cache. A partial update invalidates
that cache so a later scan can repair every command scope and the menu button.
Unchanged successful menus make no Telegram calls.

A failed recipient update waits 30 seconds before retrying the same content.
Telegram 400/403 rejections wait one hour. A fresh user interaction can retry that
recipient sooner. Changed menu content can also retry sooner. Telegram 429 pauses
all menu synchronization for at least `retry_after` (minimum 30 seconds), including
fallback-menu clearing; user interactions cannot bypass this pause. Other bot work
continues. These deadlines and caches are in memory and reset on restart.

The scanner retains its cursor if the page deadline expires during an update.
Later pages therefore do not lose recipients to a slow or cancelled attempt.
Menu warnings include the failed operation, status, outcome, fixed reason class,
and retry delay when available. They exclude recipient IDs and raw provider errors.

## Unavailable Telegram recipients

A confirmed private-chat 403 with `user_deactivated` or `bot_blocked` persists a
transport pause for that bot and recipient. Other errors, including `chat_not_found`,
429, timeouts and unrelated 403 responses, do not create this state. Bans, IAM access,
profiles, history, memory and business records stay unchanged.

Paused recipients leave menu and activity scans. Messages, media, reactions and
other private-chat effects check the same persistent gate. Task attempts and queued
notifications wait with a one-minute local recheck before doing identity/model work.
An already-dispatched external operation can finish; the pause does not undo its
business effects, cancel its run or replay an uncertain delivery. Shared catalog
maintenance and business consistency work remain independent of Telegram reachability.

A newly accepted private Telegram message or callback clears the pause in the intake
transaction. Duplicate updates and rolled-back acceptance do not clear it. A revision
fence prevents an older send failure from reinstating the pause after a fresh reply.
Pending work resumes on its next local check. A previously rejected or uncertain send
keeps its existing delivery receipt and is not automatically replayed.

A banned user still receives the required static access-denied reply to an incoming
request; this exception does not resume tasks or change their ban or transport state.
The state survives restarts. Apply schema migration 79 before starting this version;
rollback refuses to drop active transport pauses. Application rollback can retain the
additive table, but older application code does not enforce these pauses.

## Tests and builds

Set ZNS_CHATBOT_TEST_DATABASE_URL to a disposable PostgreSQL database owned by the runner. Tests create isolated schemas. Use a focused test selection while changing a contract; required CI checks cover the complete supported packages. Race checks need CGO and a C compiler; the release binary uses CGO disabled.

```sh
go test -count=1 -p 1 -parallel 4 ./cmd/... ./internal/... ./migrations/... ./integration/...
CGO_ENABLED=1 go test -race -count=1 -p 1 -parallel 4 ./cmd/... ./internal/... ./migrations/...
python3 tools/test-integration.py NEW_OUTPUT_DIRECTORY
golangci-lint run ./cmd/... ./internal/... ./migrations/... ./integration/...
CGO_ENABLED=0 go build -o zinusya ./cmd/shuttle
```

Run database-heavy commands sequentially within the available connection and memory budget. The [integration runner](shuttle-integration-batches.md) bounds the complete PostgreSQL package and verifies exact discovery coverage. A successful subset does not establish complete-suite acceptance. Use the [QA procedure](shuttle-qa.md) for native/agent behavior and the [roadmap](shuttle-roadmap.md) for remaining work. Concrete installation configuration is maintained separately.

## Source formatting

Maintained Go code and tests use a 120-character line limit, including string
literals and comments. Both the formatter and `lll` count a tab as one character.
Generated code follows its generator. Keep struct tag separators compact; the
formatter does not pad tags.

Split long strings at words, sentences, SQL clauses or JSON property boundaries.
Preserve the exact runtime bytes, including spaces, newlines and Unicode.
Use named constants for long reusable descriptions. Keep schema descriptions and
validation equivalent when moving metadata out of a struct tag. Do not exempt
strings from the limit or suppress a length check to hide unreadable content.

Release binaries: `zinusya`, `zinusya-import`, `zinusya-symbols`, and `zinusya-avatar`.
Shuttle names the implementation version; source and configuration paths retain that name.
