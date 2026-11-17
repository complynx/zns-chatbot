# Developer workflow

Use the Go toolchain pinned by go.mod. The application keeps typed startup configuration and secrets separate from durable business settings. Copy the example for a private developer configuration and supply the required ZNS_CHATBOT_* secret environment variables. Do not commit the populated configuration.

```sh
cp config/zinusya.example.yaml config/zinusya.yaml
go build -o /tmp/zinusya-avatar ./cmd/zinusya-avatar
ZNS_CHATBOT_AVATAR_EXECUTABLE=/tmp/zinusya-avatar go run ./cmd/shuttle -config config/zinusya.yaml
```

For an explicit operator-only installation, `-migrate` can finish runtime grants
and one-time settings initialization, then exit:

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

## Ordinary startup and restart

ZiNuSya operates on retained production data. Ordinary startup must apply pending
application and River migrations before loading runtime settings or composing runtime
services. It then checks the installed schema and runtime access before workers,
listeners or webhook setup. A migration failure stops startup; inspect its actual
committed history before recovery. Do not use an automatic Down or rerun an import.

Set `migration_database_url` or `ZNS_CHATBOT_MIGRATION_DATABASE_URL` for the dedicated
startup migrator. Keep the runtime credential limited to application work. The
migration stage has a five-minute budget independent of the HTTP probe timeout.
Verify the actual database/schema target across the two connections, grant the
runtime role derived from its live connection access to new objects, then close
the migration connection. Existing settings stay unchanged.

Without a separate migration credential, an already current schema and sufficient
runtime access permit startup without DDL. If migration is required, the fallback
runtime connection must have migration rights; use the dedicated credential for
production role separation. See the [schema contract](shuttle-schema.md)
and [live-upgrade gate](shuttle-qa.md#live-upgrades-and-restarts). Verify this startup
path on the candidate binary before claiming it is active in an installed release.

The explicit `-migrate` mode remains available for operator preparation and exits
without starting runtime services. `-check-config` stays offline and applies no
migrations. These modes are not a substitute for normal-startup acceptance.

The avatar renderer is a separate trusted executable; its absolute path must be
available before processing. Startup validates bot/IAM identity and the database
binding. One application instance owns its task schema; the instance lease requires
at least two task-pool connections.

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

## Loom step-limit pauses

A Loom run paused with `step_limit` stays attached to its conversation. ZiNuSya
sends one notice with Continue and Stop reply buttons for each exhausted budget.
The notice replaces a question or approval that the run retains. Loom's
`step_budget_start` identifies the budget, so polls and restarts do not repeat
the notice, and a later exhausted budget notifies again.

Both buttons send ordinary conversation text. Loom renews the budget and wakes
the same run ([Loom PR67](https://github.com/complynx/loom/pull/67)); the agent
decides what to do with the answer. ZiNuSya does not resume, reset or replace the run.

A pause request, a cancel request or an unresolved step outcome keeps the run
paused after Loom accepts an input. ZiNuSya then confirms that the message was
sent and does not repeat the notice.

The task that owns the run retires the buttons when it next polls the run. A
notice delivered late can thus show buttons for a short time after the run moved on.

## Spent run allowance and lost runs

A run paused with `budget_exhausted` spent its own money allowance. ZiNuSya shows
the same Continue/Stop notice, with the progress note that Loom saved as the run
result. Loom does not wake such a run from an input. ZiNuSya sends the customer's
reply to the chat inbox instead
([Loom conversation recovery](https://github.com/complynx/loom/blob/main/docs/api/conversation-recovery.md)).
Loom cancels the run as `superseded` and admits a successor with the same
authorization session. The task that owns the run then follows the successor and
delivers its result. The reply task completes only when the owner observes the
run that received the reply. If the paused run ended for another cause, Loom
starts a new round and the owner task ends; the reply task then observes that
round itself.

Loom can fail to give a successor the operation receipts of the replaced run
(pauses `recovery_context_too_large` and `recovery_context_unavailable`, and
`waiting_auth / recovery_authorization_required`). No input and no session
renewal continues such a run. Its owner task ends with a failure notice, and
the next customer message starts a new run as described below.

An inbox message that Loom keeps without delivery blocks all later messages of
the chat. ZiNuSya waits at most five minutes while Loom stops the replaced run
or the session renews. After that, and for other blockers, it withdraws the
message and tells the customer. The next customer message is a new attempt.
If the owner task already ended, the withdrawn text starts its own run. A text
that Loom blocked with `inference_or_effect_uncertain` never starts new work.

A run paused with `credits_exhausted` means an empty wallet. ZiNuSya tells the
customer once for each failed attempt. The next message wakes the run.

A 4xx response without a Loom error code comes from a proxy. It does not prove
that a run is gone or that Loom rejected a request, so the task keeps its state
and retries. These retries have no time limit.

A new request can find the default chat busy (`chat_busy` or
`pending_conversation_messages`) while no local task observes the run. This
occurs after the observer failed or an operator closed it. That run cannot
deliver a result. ZiNuSya cancels it, withdraws a retained message, and creates
the new run in the same chat, which keeps the history. Cancellation does not
undo completed actions. If the chat stays busy for five minutes, the request
fails with the usual notice. A run with an unknown effect (Loom reports
`inference_or_effect_uncertain`, or the run is `reconciling`) is not cancelled:
the request fails, and an operator must reconcile the run in Loom. For other
runs the cancellation is an accepted risk: a cancelled
run can be in the middle of an action, and the new run gets the chat history
but no operation receipts. Protect a critical business action with a
transaction or idempotency in its own API.

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
The state survives restarts. Startup must finish schema migration 79 before runtime
work; rollback refuses to drop active transport pauses. Application rollback can retain the
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
