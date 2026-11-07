# Agent closure of uncertain local tasks

`zinusya_administration.close_task` is a global administrator write. It closes local
processing after an administrator inspects the diagnostics and external outcome.
It does not establish whether Telegram delivered a message or Loom completed an
external action. It does not resend messages, cancel a remote run or change money.

## Protocol

Arguments are `task_id`, `expected_version` and a nonblank `reason` of at most 512
characters. Surrounding whitespace is trimmed before binding the proposal and
result. The agent must inspect the target before asking for approval;
diagnostics alone are not consent.

POST `/internal/agent/administration/close_task/prepare` with protocol version 1,
operation ID and arguments. Preparation persists a random proposal ID, normalized
argument hash, description and absolute five-minute expiry using database time.
The description identifies the local task, account, observed state, version,
reason and effects. Preparation does not close or lease the task.

POST `/internal/agent/administration/close_task` with the same operation ID,
matching idempotency key, arguments and confirmation:

```json
{
  "mode": "exact_parameters",
  "preconditions": {"proposal_id": "<prepared proposal UUID>"}
}
```

Loom collects exact approval before calling the write. Confirmation checks the
current target version, uncertain evidence, absence of a live lease, proposal
identity, argument hash and expiry. The task closure, original task-resolution
receipt and agent result commit in one transaction. A failure saving the agent
result rolls back all closure effects.

Only pending/retryable delivery rows become blocked. Existing unknown/inflight
evidence and remote identifiers remain; normal startup recovery can subsequently
classify an abandoned inflight delivery as unknown. Clearing conversation state
is conditional on that conversation still pointing to this task.

## Retry and authorization

POST the same protocol version and operation ID to `/status` or `/reconcile` under
the method path. A known result returns `succeeded` with the original receipt even
after proposal expiry, task version changes or application restart. A lost HTTP
response is not grounds to create a replacement operation. `not_found` is not
proof of an external failure or a safe resend.

Repeated preparation returns its original description, proposal and expiry; it
does not renew the five-minute window. Reusing an operation with changed arguments
conflicts. An existing operator receipt is never adopted as an agent result. A
new agent mutation rejects a colliding `(task_id, operation_id)` operator receipt.

The proposal namespace is `(bot, organization, actor, operation)`. Every route
checks current administrator authorization using the configured IAM cache policy
and requires an active local account. Transactions lock that exact local actor
against concurrent local revocation. They do not claim instantaneous detection
of remote IAM changes. Reads and replay also verify the target's current
bot/organization scope. Another actor cannot read this actor's proposal receipt.

## Migration and rollback

Migration 28 adds `shuttle_task_closure_proposals` without changing existing
operator receipts. Binary rollback keeps that table and its audit data. Schema
downgrade is allowed only while the table is empty; the migration refuses to
discard any prepared proposal or result. Export and reconcile audit history
before intentional removal. A database dump belongs in the rollout procedure.

Settings changes have a separate [agent write contract](shuttle-settings-changes.md).
Account revocation remains a later agent write slice.
This feature is not a production readiness declaration.
