# Run the first Shuttle slice

Use Go 1.27.2. Copy config/shuttle.example.yaml to config/shuttle.yaml and provide
secrets through SHUTTLE_* environment variables. Runtime business settings are
stored in PostgreSQL and are not reset from YAML at startup.

```sh
go run ./cmd/shuttle -config config/shuttle.yaml -migrate
go run ./cmd/shuttle -config config/shuttle.yaml
```

Migrations run as an explicit release step. Run one application instance in this
first slice; a database advisory lock rejects a concurrent instance. Startup
reconciles delivery attempts from the previous instance.
Configure Telegram webhook to POST /telegram/webhook with the configured secret.
Webhook registration is an operator action, not an automatic startup mutation.

The public listener serves health, readiness and Telegram ingress. The internal
listener serves agent tools, task status, administrative APIs and /metrics. Keep
it on the private Docker network or loopback. Agent/admin endpoints also require
verified bearer credentials. Readiness requires PostgreSQL, not IAM or Loom:
accepted work can wait for those dependencies.

```sh
SHUTTLE_TEST_DATABASE_URL=postgres://shuttle:shuttle_test_only@127.0.0.1:55473/shuttle?sslmode=disable go test ./cmd/... ./internal/... ./migrations/... ./integration/...
go test -race ./cmd/... ./internal/... ./migrations/... ./integration/...
golangci-lint run ./cmd/... ./internal/... ./migrations/... ./integration/...
```

Database tests create isolated schemas. The race command needs a compiler and
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

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
