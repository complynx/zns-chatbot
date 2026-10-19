# Run the first Shuttle slice

Use Go 1.27.2. Copy config/shuttle.example.yaml to config/shuttle.yaml and provide
secrets through SHUTTLE_* environment variables. Runtime business settings are
stored in PostgreSQL and are not reset from YAML at startup.

The task-service PostgreSQL pool requires at least two connections: the instance
lease retains one, and startup/work needs another. A smaller configured pool fails
construction. One-slot DB/API-only stores remain supported. See the
[pool startup guard and behavioral proof](shuttle-task-pool-startup.md).

Shuttle CI runs on branch pushes and pull requests using Go 1.27.2 and isolated
PostgreSQL. It verifies the reviewed dependency lock, behavioral/race tests,
generated SQL, lint and Linux/Docker builds. It does not publish or deploy images.

```sh
go run ./cmd/shuttle -config config/shuttle.yaml -migrate
go build -o /tmp/zinusya-avatar ./cmd/zinusya-avatar
SHUTTLE_AVATAR_EXECUTABLE=/tmp/zinusya-avatar go run ./cmd/shuttle -config config/shuttle.yaml
```

Migrations run as an explicit release step. Run one application instance in this
first slice; a database advisory lock rejects a concurrent instance. Startup
reconciles delivery attempts from the previous instance. After SIGKILL, River
also rescues abandoned running jobs using a timeout derived from the task lease
and job timeout; its scan cadence is 30 seconds. Allow rescue/retry time beyond
process readiness.
Configure Telegram webhook to POST /telegram/webhook with the configured secret.
Callback acknowledgements are bounded and do not change durable acceptance.
Permanently unsupported private callbacks return 200 with an ignored result;
persistence failures still return 503.
Webhook registration is an operator action, not an automatic startup mutation.

The public listener serves health, readiness and Telegram ingress. The internal
listener serves agent tools, task status, administrative APIs and /metrics. Keep
it on the private Docker network or loopback. Agent/admin endpoints also require
verified bearer credentials. Readiness requires PostgreSQL, not IAM or Loom:
accepted work can wait for those dependencies.

Optional Mini App account routes also use the public listener. After migration
24, enable them with `mini_app_origin` / `SHUTTLE_MINI_APP_ORIGIN`, one exact HTTPS
origin without a path or trailing slash. Empty disables them. See
[the session protocol and acceptance limits](shuttle-miniapp.md); enabling routes
does not configure a Telegram launch or make the private agent listener public.

```sh
export SHUTTLE_TEST_DATABASE_URL=postgres://shuttle:shuttle_test_only@127.0.0.1:55473/shuttle?sslmode=disable
go test -parallel 4 ./cmd/... ./internal/... ./migrations/... ./integration/...
CGO_ENABLED=1 go test -race -parallel 4 ./cmd/... ./internal/... ./migrations/... ./integration/...
golangci-lint run ./cmd/... ./internal/... ./migrations/... ./integration/...
```

Provision a disposable PostgreSQL database at the example DSN first. The test
role needs CREATE SCHEMA permission; never point this DSN at production.
Database tests create isolated schemas. Run these commands sequentially so they
do not compete for the same database connection budget. The parallel limit fits
the tested 100-connection PostgreSQL stand; this is not a memory guarantee.
A single integration process exceeded a 1.5 GiB container limit. On constrained
local stands, enumerate the complete test list and run bounded groups in fresh
processes/containers, checking exact coverage rather than selecting a convenient
subset. GOMEMLIMIT is a soft Go-runtime target, not a bound on total process memory. See the
[combined verification and retained failures](shuttle-combined-qa-2026-10-15.md).
The race command needs a compiler and
CGO_ENABLED=1; the release binary uses CGO_ENABLED=0. Build Docker from repository
root with deploy/shuttle/Dockerfile. Compose example has a five-second stop limit;
application shutdown targets one second. The example does not provision secrets,
register webhooks or modify the existing Python bot deployment.

The initial runtime settings include auth-cache TTL and fallback language. Worker
concurrency takes effect on restart; the stored question-timeout setting does not
change Loom prompt expiry. The separate optional `name_expectation_seconds`
setting controls new full-name requests (default 7200; explicit 60–86400).
See [owner profile and expectations](shuttle-profile-expectations.md).
The Docker image contains both executables. Manual runs need the trusted absolute
renderer path shown above; see [avatar tools and limits](shuttle-avatars.md).
Account.me and the role-gated
[administrative reads](shuttle-agent-administration.md) are advertised agent tools.
Current business-tool progress is recorded in the [roadmap](shuttle-roadmap.md),
including registration, pairs, tiers, confirmed payments and exact operator writes.
Private PostgreSQL pool gauges are implemented and accepted; see
[metrics semantics and source/native checks](shuttle-pool-metrics.md).
The [unresolved-task snapshot](shuttle-task-metrics.md) adds bounded counts and
accepted-task ages with failure/freshness metadata. Its occasional database refresh
has a separate budget; it does not measure runnable River latency or outbox backlog.
The independent [outbox snapshot](shuttle-outbox-metrics.md) covers unfinished
chunks and external uncertainty. Its separate query/cache preserves task-family
health. The [River due-job snapshot](shuttle-queue-metrics.md) separately reports
default-queue scheduling backlog for three fixed job kinds and states. It measures
lateness against `scheduled_at`, including overdue polls; scheduled/retryable jobs
still require promotion. If all three collectors are due, a scrape can incur three
250 ms database budgets plus cancellation and SDK overhead. Check each family's
success and freshness alongside its counts. A 200 response alone is insufficient.
Readiness and available-job execution do not prove maintainer leadership or
scheduled/retryable promotion. An unexpired abandoned River lease can delay that
promotion after restart. A bounded native [expiry-recovery probe](shuttle-river-startup-lease.md)
passed; the original fast start/stop lease observation remains unresolved.
The [identity cache counters](shuttle-identity-metrics.md) separately describe
trusted positive lookups and successful renewal fallback decisions. They reset
on bot restart and never contact IAM while scraping. Dashboards and alerts remain
later observability work; consult the linked verification checkpoint before rollout.
Existing telemetry also exports HTTP/runtime/attempt/delivery
metrics and trace propagation. File references and bounded processing are described in
the [file contract](shuttle-files.md).
The owned stand now uses committed upstream Loom with accepted operation sealing
and standard-font PDF rendering; see [consumer QA and image provenance](shuttle-loom-upstream-qa-2026-10-15.md).

See [step 0 operator recovery](shuttle-step-0.md) for private task diagnostics,
confirmed local closure of uncertain tasks and migration/rollback constraints.
The agent [close_task contract](shuttle-task-closures.md) adds exact approval and
durable status/reconcile and introduced schema 28. The
[agent settings write](shuttle-settings-changes.md) requires schema 29. Run the
candidate image with `-migrate` before starting it; ordinary startup does not
migrate. Binary rollback retains proposal tables; schema downgrade refuses to
discard proposals or results. A replayed settings receipt describes its original
version; current process IAM TTL follows current database settings.

Agent [local user revocation](shuttle-user-revocations.md) requires schema 30,
with exact approval, original receipts and read-only recovery by another active
administrator after self-revocation. Further calls by the revoked owner remain
denied. See the [native acceptance limits](shuttle-user-revocation-qa-2026-10-15.md),
including the current Loom discovery pause after self-revocation.

Worker callback duration now uses the private OTel histogram described in
[its contract](shuttle-worker-duration.md). A nil callback return is not a
delivery or business-success guarantee. The process-local series reset on bot
restart; [native QA](shuttle-worker-duration-qa-2026-10-15.md) covers all three
callback wrappers through safe no-op paths. That acceptance checkpoint used
schema 30 / manifest `mvp-29`.

Agent event configuration requires migration 31 and manifest `mvp-30`. See
[the catalogue and exact-write contract](shuttle-events.md) and
[current QA status](shuttle-event-change-qa-2026-10-15.md). Run the explicit
migration command before starting this binary. Keep proposals when rolling back
the executable; schema downgrade refuses any proposal history. An uncertain
write is reconciled with its original operation ID, never a fresh replacement.
Existing Telegram approvals with parameters receive a new full-parameter
presentation. Use the newest buttons; known-unapplied old callbacks are rejected,
while uncertain requests retain their original idempotent reconciliation.

See [the server rollout guide](shuttle-server-rollout.md) for the complynx.net
pilot plan and deployment gates.
The [initial messaging review and QA report](shuttle-review-qa-2026-10-12.md) records the
accepted messaging-stage scope, measured recovery and remaining release gates.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk

The queue schema is pinned to bot ID, IAM organization and issuer before worker
recovery; mismatched startup fails without rewriting accepted work. Apply
migration 00004 explicitly. See [deployment scope](shuttle-deployment-scope.md).

The portable [Zinusya operations dashboard](shuttle-dashboard.md) uses the existing
private metric families and explicit deployment labels. Import and datasource
selection are separate from alert activation or production rollout.

Confirmed [final-response latency](shuttle-response-latency.md) requires migration
32 before the new executable. Keep its additive columns during binary rollback;
schema rollback refuses to erase accepted timing facts. The histogram resets on
restart and is not reconstructed from historical receipts. See the
[native QA limits](shuttle-response-latency-qa-2026-10-15.md).
