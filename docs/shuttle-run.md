# Developer workflow

Use the Go toolchain pinned by go.mod. The application keeps typed startup configuration and secrets separate from durable business settings. Copy the example for a private developer configuration and supply the required SHUTTLE_* secret environment variables. Do not commit the populated configuration.

```sh
cp config/shuttle.example.yaml config/shuttle.yaml
go build -o /tmp/zinusya-avatar ./cmd/zinusya-avatar
SHUTTLE_AVATAR_EXECUTABLE=/tmp/zinusya-avatar go run ./cmd/shuttle -config config/shuttle.yaml -migrate
```

Apply migrations explicitly before ordinary startup. After the schema is ready, run the same application command without -migrate. The avatar renderer is a separate trusted executable; its absolute path must be available before processing. Startup validates bot/IAM identity and the database binding. One application instance owns its task schema; the instance lease requires at least two task-pool connections.

Webhook registration is an operator action, never an automatic startup mutation. Readiness requires PostgreSQL; IAM or Loom downtime can leave accepted work waiting. A graceful stop preserves task ownership and uncertain external effects for recovery. See the [architecture](shuttle-architecture.md), [schema baseline](shuttle-schema.md) and [deployment identity invariant](shuttle-deployment-scope.md).

## Tests and builds

Set SHUTTLE_TEST_DATABASE_URL to a disposable PostgreSQL database owned by the runner. Tests create isolated schemas. Use a focused test selection while changing a contract; required CI checks cover the complete supported packages. Race checks need CGO and a C compiler; the release binary uses CGO disabled.

```sh
go test -parallel 4 ./cmd/... ./internal/... ./migrations/... ./integration/...
CGO_ENABLED=1 go test -race -parallel 4 ./cmd/... ./internal/... ./migrations/...
python3 tools/test-integration.py NEW_OUTPUT_DIRECTORY
golangci-lint run ./cmd/... ./internal/... ./migrations/... ./integration/...
CGO_ENABLED=0 go build ./cmd/shuttle
```

Run database-heavy commands sequentially within the available connection and memory budget. The [integration runner](shuttle-integration-batches.md) bounds the complete PostgreSQL package and verifies exact discovery coverage. A successful subset does not establish complete-suite acceptance. Use the [QA procedure](shuttle-qa.md) for native/agent behavior and the [roadmap](shuttle-roadmap.md) for remaining work. Concrete installation configuration is maintained separately.
