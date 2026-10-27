# Agent task closure plan

The existing operator endpoint can close an uncertain local task. The agent can
inspect it but cannot request the same action through Loom's write protocol.
Expose one `zinusya_administration.close_task` method with current global-admin
authorization and exact-parameter approval. This slice does not retry delivery,
cancel a remote Loom run, infer an external result or change financial records.

Architecture and Go consultations agreed on DB-owned preparation, confirmation
and receipt recovery. Keep API authorization/envelopes with existing agent writes.
Arguments are only `task_id`, `expected_version` and a bounded, nonblank reason.
The human description identifies the target, observed uncertainty, approved version,
reason and exact local effects. Preparation holds no task or worker lease while
waiting for a decision.

## Transaction and compatibility

First extract the existing closure effects into a private transaction-owned helper:
uncertainty validation, versioned close/fence advance, pending/retryable delivery
blocking, conditional conversation clearing and original resolution insertion.
The operator wrapper retains its current task locking, receipt replay and commit.
The agent wrapper owns its own replay and commits the same effects with its
proposal result in one transaction. Do not nest the operator transaction inside
the agent transaction or copy its business logic.

A dedicated additive proposal table is required: global task closure has no event,
so event-bound proposal tables are unsuitable. Key it by bot, organization, actor
and operation ID. Persist normalized arguments/hash, random proposal UUID, original
preparation document, five-minute expiry and nullable committed result. Use SQLC.
An exact repeated preparation returns the original document and expiry; changed
arguments conflict. Expiry does not silently renew an operation.

Serialize operation → lock matching active local actor for share → lock scoped
target/effects. Every HTTP call independently verifies the current IAM admin role;
the actor lock covers local revocation, not IAM's remote role state. Recheck target
scope and, before first application, version, uncertainty, live lease and database
time after locking. Known applied proposals replay their original result despite
expiry or version advance. Status/reconcile remain actor/deployment scoped and do
not expose unprepared operator receipts.

The existing task-resolution key is `(task_id,operation_id)`, narrower than the
agent proposal namespace. Before a new agent mutation, reject any existing
resolution with that key; never adopt an operator-created receipt as agent
success. Agent replay comes only from its own proposal's committed result.

Keep unknown/inflight delivery evidence and remote identifiers. Add no River
deletion, remote cancellation or financial effect. Existing task/conversation
lock-order risks are not solved by this extraction and must not be described as
solved. Pending work must still reject rather than close a currently leased task.

The new proposal table must leave existing operator receipts readable and the
previous application runnable. Rollback must retain/export approval/result audit;
do not drop applied proposals merely to downgrade a binary. Actual migration,
rollback and old-image compatibility need their own proof before acceptance.

## Acceptance and current boundary

Behavioral checks must cover prepare/confirm/status and exact replay after expiry
and restart; wrong actor/bot/organization, local revocation, missing/downgraded
confirmation, changed arguments/proposal, expired/stale/leased/resolved targets,
operator receipt collision and simultaneous confirmation. Verify one closure,
original receipt, conditional conversation clearing, preserved uncertain evidence,
blocked pending chunks and rollback if result persistence fails. Manifest/discovery
and native real IAM/Loom exact approval must complete the same method.

The preserving extraction, migration 28, durable proposal store, agent routes and
manifest discovery are implemented.
Focused PostgreSQL race checks cover the
critical paths above.
An HTTP test drops the response after the real handler
commits, then checks status/reconcile after restart without repeating closure.
Settings changes have their own [agent write contract](shuttle-settings-changes.md).
Account revocation remains a separate agent write slice.
This plan is not production readiness.
