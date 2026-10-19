# Local user revocation

The agent administration module now implements `user`, `revoke_user` and
`user_revocation_receipt`. These require the current delegated user's global
`zinusya.admin` role and an active local account in this deployment. They revoke
local access only. They do not change remote Zitadel roles, restore accounts or
coordinate caches in other bot processes.

## Exact approval

`user({"id":"<user UUID>"})` reads only `user_id`, `expected_subject` and
`expected_revoked` in the verified bot/organization. It is not a directory or
profile editor. An empty subject is valid for a scoped account not yet bound to
IAM. Foreign accounts are hidden.

Prepare with POST `/internal/agent/administration/revoke_user/prepare`, using the
normal Loom envelope and those three arguments. Preparation stores the original
arguments, description, random proposal UUID and a five-minute database expiry.
It makes no account change. A retry with the same actor, operation and arguments
returns the original preparation; different arguments conflict.

Confirm at `/internal/agent/administration/revoke_user` with the same arguments,
matching `operation_id` and `idempotency_key`, and
`confirmation.mode="exact_parameters"` with the original
`confirmation.preconditions.proposal_id`. The target's subject and revoked flag
must still match. Expiry uses database wall time after any user-lock wait. There
is no account business revision; this tuple does not detect a future revoke/
restore/rebind sequence that returns to the same tuple.

Confirmation commits local revocation, identity audit and original result in one
transaction. Concurrent deliveries of one operation create one audit. A fresh
explicit operation may revoke an already revoked target and records another
audit, preserving the operator API's behavior. The result contains `user_id`,
`subject` and `revoked=true`.

## Recovery and self-revocation

Status and reconcile use POST at `/internal/agent/administration/revoke_user/status`
and `/reconcile`, with the original actor and operation ID. They return the
original committed receipt independently of later target changes. A missing
receipt returns `not_found`; this is not permission to retry under a new operation.
After a lost response or recoverable 503, keep the same operation ID.

Self-revocation is permitted and preparation explicitly warns about loss of access.
The initial confirmation can return its receipt. All subsequent calls by that
revoked account remain denied, including status and replay. A lost self-revocation
response does not create an authentication exception.

With the current Loom broker, the next discovery call from that revoked account
pauses its run with `module_discovery_unavailable`. The committed tool result
remains valid; a final owner model response is not guaranteed. Recovery uses the
other administrator below, without restoring the revoked account's access.

Another active global administrator in the same deployment may call
`user_revocation_receipt({"proposal_id":"<original proposal UUID>"})`. This is a
separate read-only diagnostic: it returns the original `actor`, `operation_id`,
`proposal_id` and, when committed, `result`. An omitted result means pending in
that read snapshot. The reader cannot take over or confirm the original actor's
operation. Retain the proposal UUID when preparing self-revocation.

## Transactions and caches

Writers acquire a namespaced operation advisory lock, then actor and target rows
`FOR UPDATE` in UUID order, deduplicating self-revocation. They validate the active
actor and target deployment inside the transaction. This avoids deadlocks during
reciprocal administrator revocations and self-lock upgrades. The operator endpoint
`POST /internal/admin/users/{id}/revoke` shares this transactional body and retains
its existing immediate, repeated-audit behavior.

After a write attempt the API invalidates trusted target metadata even when the
PostgreSQL COMMIT reply was lost. An error still means the result is uncertain;
metadata returned with that error is not proof of success. Replay and diagnostic
recovery invalidate the original receipt subject and the current target subject
when that target remains in this bot/organization. A scoped target that has moved
or disappeared does not expose its new foreign identity. Failed current-state
readback returns a recoverable 503; it does not claim rollback. Cache invalidation
is process-local. Local revoked-account checks remain independent of IAM caches.

Migration 30 adds the dedicated durable proposal/result table. Its down migration
works only while that table is empty; pending preparations and committed receipts
both require export before destructive downgrade. This prevents loss of approval
and audit history.

## Verification boundary

Focused real-PostgreSQL behavioral race tests cover exact approval, stale state,
expiry after a lock wait, concurrent delivery, reciprocal/self operator writes,
atomic rollback, scope and role denial, restart, historical receipts, and cache
invalidation after identity rebinding. The COMMIT-loss tests drop an actual
PostgreSQL COMMIT acknowledgment after the database commits, separately for
preparation, confirmation and self-revocation recovery. Fresh independent store
and API correctness/quality reviews found no actionable defects.

The overall regression gate passed: all 221 listed integration scenarios and
the other explicit Go packages passed under the race detector, with zero-issue
strict lint and matching pinned sqlc output. Fifteen bounded integration batches,
enumeration and package checks produced 17 successful container exits without OOM.
All 304 frozen source hashes remained unchanged through the gate.

Real Zitadel/Loom exact approval, other-administrator recovery, restart and
previous-binary compatibility with retained schema 30 passed on the owned stand.
The accepted stand now runs schema 30 / manifest `mvp-29`. Self-revocation was
accepted at tool-result/owner-denial/independent-receipt level; its paused run was
cancelled for cleanup, with no final owner model response claimed. See the
[native QA record, retained failures and limits](shuttle-user-revocation-qa-2026-10-15.md).
Production has not been changed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
