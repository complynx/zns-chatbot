# Dependencies and tools

Use small domain modules and the standard library for transport and infrastructure. Add a dependency only with code that needs it. The actual graph is defined by go.mod, go.sum and tools/dependencies.lock.json; documentation must not become a second version lock.

## Stack and ownership

| Concern | Choice | Boundary |
| --- | --- | --- |
| PostgreSQL | pgx and sqlc | Typed queries and bounded pools; business transactions stay explicit. |
| Migrations | Goose | Embedded application schema; River owns its own migrations. |
| HTTP API | net/http and Huma | No additional router or ORM is needed. |
| Durable jobs | River | Scheduling and execution leases; business idempotency and external-effect reconciliation remain Shuttle responsibilities. |
| JWT and JWKS | go-jose with typed net/http adapters | Crypto does not replace issuer, audience, organization and current-authority checks. |
| Configuration | YAML with typed environment overrides | Startup inputs and secrets remain separate from durable business settings. |
| Telegram rendering | Goldmark AST and a small entities renderer | Preserve text and UTF-16 references; HTML is not an intermediate Telegram wire protocol. |
| Telemetry | OTel API/SDK and Prometheus exporter | One application metric family; private bounded metadata without content or credentials. |

The Telegram client is a narrow typed net/http adapter for the methods the application uses. Inbox commit, polling offset, webhook acknowledgement, cancellation and file limits must remain explicit. Bot-token paths must be sanitized before telemetry export.

Generic file tools belong to Loom. Shuttle exposes authorized attachment references and bounded reads. The isolated avatar executable uses a versioned contract; heavy processing does not move business state into the worker. Add converters, a model SDK or another workflow engine only for a proven requirement.

## Dependency changes

Pin canonical versions and content hashes. The reviewed lock records full VCS revisions for the allowed graph. Avoid floating versions, unreviewed replacements and implicit upgrades. Run module verification, lock checks, applicable vulnerability/license checks, lint, builds and focused behavioral tests. Read vulnerability findings rather than interpreting a process exit alone as a clean result.

The project accepts MIT, Apache-2.0, BSD-2/3-Clause, ISC and MPL-2.0 subject to their notices and conditions. Unknown/custom licenses need an explicit decision. Preserve the Docker build's dependency notice inputs and covered-source obligations; generated audit receipts are kept separately from maintained documentation.

## CI and local validation

The [bounded integration runner](shuttle-integration-batches.md) runs PostgreSQL tests in separate processes. Concurrent shards need separate disposable databases. Its evidence verification mode checks shard coverage without requiring PostgreSQL or Go. Race checks require CGO and a C compiler; the release binary uses CGO disabled.

Keep exact tool/action versions in the workflow and lock files. CI artifacts are execution evidence, not runtime dependencies. See the [QA procedure](shuttle-qa.md) for evidence and acceptance limits.
