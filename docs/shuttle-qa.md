# Behavioral acceptance

Use the specification and domain contracts as the checklist. Record each requirement against the exact source, build and dependency versions used for that observation. Source review, developer tests, controlled orchestration, actual model behavior, real Telegram behavior and release readiness are separate evidence levels.

## Live upgrades and restarts

ZiNuSya already runs in production. Test schema/startup changes against an owned,
populated copy or representative synthetic records. Use production copies only under
their existing custody and privacy rules. Keep exact live inventory and run artifacts
outside tracked documentation. The rows below are release requirements, not PASS claims.

| Case | Required observation |
| --- | --- |
| Populated previous schema → candidate | Pending application and River migrations apply before runtime work. User/domain IDs, settings, history, memory references and original operation/delivery receipts retain their meaning. |
| Repeat startup with current schema | No duplicate effects, reset settings or rewritten migration history. Pending and uncertain work keeps its original identity. |
| Readiness and failure ordering | Bad credentials, wrong target, missing rights, migration failure or an unavailable database prevent workers, listeners and webhook setup. Record any already committed migration stage. |
| Credential separation | Startup DDL uses the intended migrator; runtime retains its non-DDL role and access to new objects. Migration connections close before runtime work. |
| Compatible version overlap | Where overlap or application rollback is supported, exercise the previous writer on the expanded schema and previous reader on representative new data. State unsupported behavior explicitly. |
| Restart with unresolved work | Known-safe retries use original operations. Unknown sends, paid dispatches and other uncertain effects remain held; restart does not create replacement runs or consent. |
| Rollback boundary | Keep application rollback separate from schema Down. Destructive Down guards retain live state; use a reviewed forward repair if rollback would lose data. |

Cover a fresh database as a separate case. Do not replace populated-upgrade checks
with a fresh install or weaken preservation assertions when adding a migration.
Update ordered rollback fixtures to include newer migrations before reverting an
older version. Reuse accepted unchanged business paths and run affected checks plus
required CI; a schema change does not require another full manual feature matrix.

Existing regression examples cover parts of this gate:

- [Runtime preflight](../internal/db/runtime_preflight_test.go): read-only access checks and unapplied-schema rejection.
- [Migration preflight](../internal/db/migration_preflight_test.go): role/read-only failures before DDL.
- [Baseline lifecycle and old/new name writers](../internal/db/migrations_test.go): retained objects, history, settings and transactional writer compatibility.
- [Prior schema upgrades](../internal/db/symbol_migration_test.go): retained catalog/task data.
- [Availability rollback guard](../internal/db/telegram_availability_test.go): restart persistence and active-state preservation.

These tests do not alone prove a newly introduced automatic-startup path. Its
candidate acceptance must show migration ordering and failure isolation before
external dependencies, workers, listeners or webhook mutation. After an authorized
rollout, check the exact installed image, effective schema, readiness and recovery
state. A merge may trigger automatic deployment: satisfy the migration/startup gate
before authorizing that trigger.

## Interface matrix

| Workflow family | Native only | Agent only | Native to agent | Agent to native |
| --- | --- | --- | --- | --- |
| Profile and legal registration details | Required | Required | Required | Required |
| Registration, pairs and pass assignment | Required | Required | Required | Required |
| Payment evidence, human review and top-up | Required | Required | Required | Required |
| Service orders and meals | Required | Required | Required | Required |
| Massage participant and specialist workflows | Required | Required | Required | Required |
| Avatar drafts, generation and delivery | Required | Required | Required | Required |
| Knowledge proposals, review and publication | Required | Required | Required | Required |

These cells define coverage; they do not assert a completed test run. Verify native independence with Loom unavailable. For agent paths, observe actual tool dispatch and persisted outcomes. A direct test-client mutation or seeded final business record cannot stand in for the user workflow.

## Required boundaries

Use fresh synthetic participants and an owned disposable event. Setup may configure authorities and catalogs. User registrations, choices, evidence, consent and resulting effects must come through the interface being accepted.

For each affected workflow, cover the happy path and relevant critical boundaries: stale versions, changed prices/deadlines, duplicate input, response loss, current authority loss, wrong owner/event, expired sessions, cancellation and restart. Exercise both handoffs while the original question or preparation remains pending. Preserve the original operation/proposal and immutable result. Reopening a completed link must read the original result without another effect.

Check persistent domain rows, original receipts, draft versions, outbox state and delivery recipient. Payment evidence must remain pending until authorized human review. An agent statement does not establish a bank fact or user consent.

File checks include owner isolation, explicit source loss versus transient failure, bounded reads and external-source unavailability. Audio checks compare complete transcript bytes across pages, source collection, process restart and reuse by another run; current disclosure authority must still apply. A controlled ASR stub proves the integration contract, not speech quality.

## Execution and custody

### Entry and navigation

- Compare bare `/start` with `/start /profile` and `/start <agent request>` for
  named and unnamed users. Only a bare first start shows the service welcome
  after onboarding. A deferred request runs after successful onboarding delivery,
  followed by its closing invitation. The agent keeps the response language.
- Restart between onboarding completion, delivery and continuation acceptance.
  Repeat the original update. Confirm one child request and no duplicate native
  closing. An uncertain onboarding delivery must not start the continuation.
- Send another start payload while onboarding is active. It must not introduce
  a generic welcome before the deferred request. Cancellation or an unconfirmed
  name must not dispatch it.
- Open and close registration, change pass availability and revoke an owner.
  Observe command menus update without another incoming message. An unchanged
  menu must not generate repeated Telegram settings calls. Menu API failure must
  not block business output. Check both supported command-list languages and
  inherited fallback scopes.
- Launch the Mini App with verified Telegram initData. Confirm automatic entry
  with no sign-in or sign-out controls. Ordinary users must not load native
  knowledge; authorized editors retain the existing per-operation checks.

### Local ingress recovery

`deploy/shuttle/test/receiver.py` is an owned-stand receiver with durable raw
batches, per-update receipts and a persisted acknowledgment cursor. It starts
HELD and verifies bot identity and webhook ownership before polling. Temporary
network failures, HTTP 408/429 and selected 5xx responses retry with bounded
backoff at the same cursor. Telegram `RetryAfter` is respected. Authentication,
consumer conflicts, certificate failures, invalid state and storage failures
remain held with content-free diagnostics.

A retained batch is resumed before polling, including updates without individual
receipts. Capture-only records are distinct from pending forwarding. Exact
webhook submissions may repeat only against Shuttle's durable bot/update inbox
deduplication; paid requests and uncertain message sends have no such retry
permission. Changed webhook targets or ambiguous legacy receipts need inspection.
The cursor advances only after classification or durable inbox acknowledgment and
successful local persistence. Test response loss after inbox acceptance, process
restart, storage failure and withdrawal of forwarding authority.

These fixtures prove local ingress contracts. They do not prove external model
quality or production telemetry ingestion.

| Operation | Recover automatically | Keep held |
| --- | --- | --- |
| Telegram reads | Transport, 408/429, selected 5xx; same durable cursor | Wrong bot, 401/403/409, invalid certificate |
| Shuttle inbox | Repeat exact retained update against durable dedup | Changed target, invalid ACK, legacy ambiguity |
| Cursor storage | Reuse last successfully persisted cursor | Disk or malformed-state failure |
| Descriptor reads | Same generation/run/artifacts; durable backoff | Invalid result, auth failure, unknown mutation |
| Telegram delivery | Existing known-safe outbox retry policy | Uncertain send outcome |
| Paid provider | Existing completed-response receipt | Reserved/orphaned/unknown paid dispatch |

Review a one-shot fixture before running it. Pin immutable inputs, constrain effects and external-call budgets, and persist intent before dispatch. A missing or uncertain receipt is not permission to rerun. Retain the failed original attempt; use a separately reviewed observation when final state can be proved without mutation.

After the observed flow, archive and fully decode owned databases before removing only identified fixture resources. Verify remaining resource counts independently. A successful flow with cleanup still pending is not a cleanup PASS. Keep private payloads and credentials out of public summaries.

Use focused tests for the changed contract and the required CI gates. Reuse earlier evidence only with explicit source/behavior bounds; do not imply a full matrix rerun. The [integration runner](shuttle-integration-batches.md) bounds the complete PostgreSQL suite. The [registration procedure](shuttle-registration-pass-parity-plan.md) gives one complete business journey.

Execution reports, screenshots, receipts and environment inventories belong in local evidence storage. Maintained documentation records contracts and reusable procedures. Publication and deployment remain separately authorized actions.
