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

An ordinary message rejected before any possible steering effect can start its
own run when the previous run finishes. Its saved input remains immutable.
An earlier unknown effect remains uncertain even if a later retry is rejected;
that message is not promoted into another run.

A task survives loss of the application process. Dependency outages postpone its
attempts. A new process reconciles abandoned delivery attempts before admitting
workers. Shutdown cancels requests and workers, aims for one second, and has a
five-second hard bound. PostgreSQL shutdown is outside this application contract.

## Identity and authorization gates

Synthetic email links the organization-scoped Telegram identity. The immutable
proposed Zitadel user ID permits reconciliation after lost creation responses.
No Telegram numeric ID is fabricated into an OIDC subject. Local user revocation
is checked independently of credential caches and invalidates those caches.

Pending Telegram results and prompts recheck local revocation before each send
attempt, and verify IAM authorization within the configured positive-cache TTL.
Without a valid cache, temporary IAM failure postpones delivery; explicit denial
blocks it. Revoked deliveries are durably blocked; a completed run with blocked
output records delivery failure. Access-denied notices remain deliverable.
Blocked positions do not hold later denial notices, while unknown or failed
send receipts still hold the delivery barrier.

Mock IAM verification does not establish the real Zitadel grant contract. The
[disposable real IAM acceptance](shuttle-real-iam-qa-2026-10-12.md) checks grants,
claims and project role isolation. Production permissions and bidirectional
zitadeltg linking still need acceptance. Loom currently performs online
introspection; continuity during an IAM outage also requires the matching Loom
cache behavior. A Shuttle token cache alone cannot satisfy that acceptance case.
Unknown bootstrap/reauthorization outcomes require bounded reconciliation.

Administrative roles come only from the introspection claim for the exact
Shuttle API project. The flat role claim can belong to another project and is
not an authorization source. Actor and exchanged tokens request the plural
projects:roles scope; the introspection application must belong to Shuttle.

After an abrupt process exit, River rescues running attempts after the greater
of the task lease and job timeout, plus one second. Its native rescue scan runs
on a 30-second cadence, followed by retry scheduling. This is separate from the
one-second shutdown target; recovery is not promised to finish in one second.

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
