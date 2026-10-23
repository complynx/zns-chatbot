# Agent local user revocation

Next slice: expose the existing local account revocation through exact agent
approval with durable receipts and process-local cache invalidation. Preserve
operator self-revocation and repeated-revocation audit behavior. Do not add remote
IAM role mutation, account restoration or distributed invalidation.

Architecture and Go consultations agreed on a dedicated proposal/receipt table
in migration 30 and a shared transactional revoke/audit body. Existing user rows
have no business revision. Exact arguments bind target UUID, observed subject and
observed revoked flag, with implicit verified bot/organization. An empty subject
must remain representable for an account not yet bound to IAM. A narrow authorized
user lookup supplies that snapshot; it is not a user directory or profile editor.

Preparation stores the observed identity, hash, original description, random
proposal UUID and five-minute database expiry. Display the target account and
observed state. Mutable profile text is untrusted display data, not authority.
An already revoked target may receive a fresh explicit operation and another
audit, preserving operator semantics; a changed state after preparation conflicts.
This current-state tuple does not claim detection of a future revoke/restore ABA.

Revocation needs a specialized transaction entry. Acquire the operation advisory
lock, then deduplicated actor/target user rows `FOR UPDATE` in deterministic UUID
order. Validate the exact active actor and scoped target after acquiring locks.
Do not reuse the settings helper's actor `FOR SHARE` followed by target update:
reciprocal administrator revocations and concurrent self-lock upgrades can deadlock.
The existing settings/task helper remains unchanged.

Confirmation checks exact proposal/hash and target identity/state, then samples
expiry after lock waits. Local revocation, existing identity audit and original
agent result commit atomically. Replaying a known result still verifies the
current active actor, but does not require the target to retain its old identity
or state. Repeated delivery of one committed operation creates no extra audit.
The operator API should share the narrow transaction body and recheck target
bot/organization inside the transaction rather than rely on its current pre-read.

Self-revocation remains supported. Its first confirmation can return the committed
receipt. Later calls by that revoked account remain denied, including receipt
replay; do not invent a revoked-user authentication exception. Preparation must
warn that the actor will lose further local API access. Another active global
administrator in the same bot/organization can diagnose the original result
through a distinct read-only proposal-UUID lookup. This diagnostic cannot take
over or confirm another actor's operation; its result identifies the original
actor/operation rather than adopting that operation as the reader's mutation.

After a real write attempt, invalidate the authoritative locked target subject
even on ambiguous commit. A failed transaction may safely cause invalidation.
Return trusted attempt metadata when necessary; never invalidate a subject merely
because the caller supplied its text. Receipt replay/recovery also invalidates
the stored subject and any freshly read current subject in this deployment.
An unavailable readback is recoverable 503 under the same operation, not proof
of rollback. Local revoked-account checks remain independent of IAM caching.

Behavioral acceptance covers exact preparation/replay, stale binding/state,
already-revoked fresh auditing, expiry after waits, actor/scope denial, atomic
rollback, simultaneous reciprocal and self revocations, original result after
target changes/restart, actual lost PostgreSQL COMMIT acknowledgment with cache
invalidation, and lost self-response recovery by another active administrator.
Verify the existing operator path and permission-filtered discovery.
Populated proposal/result audit must survive binary rollback and
block destructive schema downgrade.

The store and agent API are implemented in source, including migration 30,
the revoke tool and the receipt diagnostic.
The owner self-revocation run paused before its next model turn and was cancelled
for cleanup; no final model completion is claimed.
See the
[implemented contract and verification boundary](shuttle-user-revocations.md).
The accepted stand is schema 30 / manifest `mvp-29`.
No production changes are implied.
