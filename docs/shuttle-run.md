# Run the first Shuttle slice

Use Go 1.27.2. Copy config/shuttle.example.yaml to config/shuttle.yaml and provide
secrets through SHUTTLE_* environment variables. Runtime business settings are
stored in PostgreSQL and are not reset from YAML at startup.

Shuttle CI runs on branch pushes and pull requests using Go 1.27.2 and isolated
PostgreSQL. It verifies the reviewed dependency lock, behavioral/race tests,
generated SQL, lint and Linux/Docker builds. It does not publish or deploy images.

```sh
go run ./cmd/shuttle -config config/shuttle.yaml -migrate
go run ./cmd/shuttle -config config/shuttle.yaml
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
the tested 100-connection PostgreSQL stand. The race command needs a compiler and
CGO_ENABLED=1; the release binary uses CGO_ENABLED=0. Build Docker from repository
root with deploy/shuttle/Dockerfile. Compose example has a five-second stop limit;
application shutdown targets one second. The example does not provision secrets,
register webhooks or modify the existing Python bot deployment.

The initial runtime settings include auth-cache TTL and fallback language. Worker
concurrency takes effect on restart; the stored local question-timeout setting is
reserved for the expectations stage. Loom owns current prompt expiry. Account.me is
the first advertised agent tool; business and administrative tool manifests follow
with their modules. Queue-age, DB-pool, token-cache dashboards and alerts are later
observability work; the current slice exports HTTP/runtime/attempt/delivery metrics
and trace propagation. File ingestion and business registration flows are not
implemented in this slice.

See [step 0 operator recovery](shuttle-step-0.md) for private task diagnostics,
confirmed local closure of uncertain tasks and migration/rollback constraints.

See [the server rollout guide](shuttle-server-rollout.md) for the complynx.net
pilot plan and deployment gates.
The [current review and QA report](shuttle-review-qa-2026-10-12.md) records the
accepted messaging-stage scope, measured recovery and remaining release gates.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
