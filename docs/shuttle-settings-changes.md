# Agent runtime settings changes

`zinusya_administration.settings` reads the current application-wide settings.
`zinusya_administration.update_settings` replaces that full document after exact
approval. Every call requires current global administrator authorization under
the configured IAM cache policy and an active local account. Startup YAML,
environment variables and secrets are outside this API.

Implementation, independent reviews, the complete 206-test race gate and bounded
native acceptance passed. See the [dated verification record](shuttle-settings-change-qa-2026-10-15.md).
This is not production readiness.

## Prepare and confirm

Read `settings` first. Supply `expected_version` and the complete `document` to
POST `/internal/agent/administration/update_settings/prepare`, using protocol
version 1 and a stable operation ID. The document has these bounds:

| Field | Accepted values | Meaning |
| --- | --- | --- |
| `auth_cache_ttl_seconds` | 0–300 | Positive IAM authorization cache; zero disables it |
| `default_language` | `en`, `ru` | Default interface language |
| `max_parallel_tasks` | 1–128 | Worker concurrency; applies after application restart |
| `question_timeout_seconds` | 1–604800 | Reserved; does not change current Loom prompt expiry |
| `name_expectation_seconds` | 0 or 60–86400 | Zero uses 7200 seconds |
| `mini_app_max_age_seconds` | 0 or 30–900 | Zero uses 300 seconds |
| `mini_app_pending_seconds` | 0 or 30–300 | Zero uses 120 seconds |
| `mini_app_session_seconds` | 0 or 60–3600 | Zero uses 900 seconds; existing sessions do not extend |

The first four fields are required. Optional omitted values and explicit zero
have the same typed representation and existing default semantics. They are not
rewritten to explicit effective values in storage. Preparation describes previous
and requested stored/effective values, application-wide scope and timing.

Preparation persists a random proposal UUID, argument hash and five-minute
database-time expiry without changing settings or writing a settings audit.
Repeating preparation returns its original proposal, description and expiry;
changed arguments conflict. It does not silently renew an expired proposal.

Loom collects exact approval, then POSTs to the method path with the same
arguments, operation ID, matching idempotency key and confirmation:

```json
{
  "mode": "exact_parameters",
  "preconditions": {"proposal_id": "<prepared proposal UUID>"}
}
```

The transaction locks the namespaced operation and exact active local actor,
checks proposal/hash, acquires the settings row, checks the current version and
samples expiry after lock waits. It commits settings, the existing settings audit
and original agent result together. Concurrent operator/agent CAS updates have
one winner. Failure saving the result rolls back settings and audit.

## Reconciliation and IAM cache

POST protocol version and the same operation ID to `/status` or `/reconcile` under
the method path. Receipts are scoped to bot, organization and actor. A stored
result replays after expiry, restart or newer settings updates. It represents the
version/document that this operation committed, not necessarily current settings.
An operator audit alone is never adopted as an agent result.

Both write paths coordinate through this API process's settings mutex. After an
agent confirmation or receipt read, this process reads current database settings
and synchronizes its IAM TTL. It never installs the TTL from a historical receipt.
Already synchronized TTLs are left alone, so polling does not repeatedly flush
the authorization cache. Operator updates retain their existing behavior and
record the successfully applied TTL locally. Startup initializes IAM from the
persisted settings.

A failed or ambiguous commit returns a recoverable 503. The API attempts to read
current settings; if that cannot be established, the new agent path temporarily
disables its positive IAM cache until successful reconciliation. A committed
receipt remains durable even if this runtime synchronization fails. Keep the same
operation ID; neither 503 nor `not_found` proves a safe replacement write.

This process-local synchronization does not coordinate other running processes.
Local account revocation remains checked independently; this feature does not
promise instantaneous remote IAM revocation detection.

## Migration and rollout

Apply migration 29 explicitly with `-migrate` before starting the new image.
Ordinary startup does not migrate. `shuttle_settings_change_proposals` is additive;
previous operator CAS/audit and task-closure paths retain their contracts. Binary
rollback keeps proposals and receipts. Schema downgrade refuses to discard any
proposal or result; export/reconcile audit before intentional schema removal.

User revocation has its own [agent contract](shuttle-user-revocations.md),
with [bounded native acceptance](shuttle-user-revocation-qa-2026-10-15.md). Settings remain a
separate approval operation; see their
[bounded implementation plan](shuttle-settings-change-plan.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
