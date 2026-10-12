# Shuttle architecture and MVP stages

Shuttle is a standalone Go process. PostgreSQL owns accepted messages, operation
state, conversation routing and delivery receipts. River schedules attempts;
River job state is not the business source of truth. Loom owns the agent loop.

## First executable stage

The first slice accepts private Telegram text messages, provisions or resolves a
Zitadel account, obtains delegated credentials, starts a Loom run and delivers
its result through Telegram entities. The private agent API exposes account.me.
Question, answer, approval and cancellation follow the same durable intake path.
Registration, passes, payment ledgers, pairs, avatars and migration remain later
MVP stages. This slice does not claim acceptance for those business modules.

- `internal/config`: startup YAML and secret/environment overrides.
- `internal/db`: PostgreSQL state, sqlc queries and embedded Goose migrations.
- `internal/identity`: provisioning, token exchange, JWT checks and cache invalidation.
- `internal/telegram`, `internal/loom`: bounded typed HTTP adapters.
- `internal/task`: River jobs, persisted external intents and recovery.
- `internal/render`: tolerant Markdown to text and UTF-16 Telegram entities.
- `internal/api`: webhook, owner-scoped tools and administrative settings.
- `internal/observability`: scoped OTel providers and private Prometheus endpoint.
- `cmd/shuttle`: composition, explicit migrations and bounded shutdown.

## Transactions and retries

Intake commits the immutable Telegram update, the domain task and an IDs-only
River job in one transaction. Duplicate updates return the original task. A short
conversation routing transaction assigns order; it never waits for a Loom run to
finish, because answers and cancellation must remain available while it runs.

Attempts claim a finite lease and use a fencing counter when saving state. HTTP
calls run outside database transactions. Operation IDs, request snapshots and
idempotency keys are saved before external calls. Lost Loom create receipts retry
the same key. Reauthorization with a lost receipt cannot blindly increment its
version. Telegram delivery is reserved before sending: an unknown outcome is
retained for investigation and is not automatically replayed.

A task survives loss of the application process. Dependency outages postpone its
attempts. A new process reconciles abandoned delivery attempts before admitting
workers. Shutdown cancels requests and workers, aims for one second, and has a
five-second hard bound. PostgreSQL shutdown is outside this application contract.

## Identity and authorization gates

Synthetic email links the organization-scoped Telegram identity. The immutable
proposed Zitadel user ID permits reconciliation after lost creation responses.
No Telegram numeric ID is fabricated into an OIDC subject. Local user revocation
is checked independently of credential caches and invalidates those caches.

Mock IAM verification does not establish the real Zitadel grant contract. Real
Zitadel impersonation claims, audience and role settings, and bidirectional
zitadeltg linking need a separate contract gate. Loom currently performs online
introspection; continuity during an IAM outage also requires the matching Loom
cache behavior. A Shuttle token cache alone cannot satisfy that acceptance case.
Unknown bootstrap/reauthorization outcomes require bounded reconciliation.

## Verification boundaries

File ingestion is a later stage. Shuttle will persist attachment IDs and source
metadata and expose authorized metadata/read APIs. Loom receives IDs and fetches
bytes on demand. Telegram source loss remains a visible unavailable-file outcome;
restart preserves references, not a promise of permanent file availability.

Persistent Loom conversation binding and history context across separate runs
also remain a later-stage requirement. This slice stores Telegram history in
PostgreSQL but does not yet reuse a Loom chat across completed runs.

Prefer HTTP-visible happy and critical paths with real isolated PostgreSQL
schemas and mock Telegram, IAM and Loom servers. Assert durable outcomes rather
than private function structure. Include duplicate intake, lost external
receipts, restart recovery, simultaneous dependency outages, owner/role denial,
question routing, cancellation and unknown Telegram delivery. Use focused fuzzing
for Markdown bounds and Linux race/shutdown checks for the target platform.

Each stage passes an independent textual-diff review loop before its commit.
Successful unit tests are not production readiness. Business-module acceptance,
data migration, real service contracts and production cutover remain explicit.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
