# Avatar delivery administration in Mini App

Schema59 / manifest mvp68 adds native administrator HTTP operations and bounded
reactive history. The [operator form](shuttle-avatar-admin-ui.md) passed its
source/browser gate. [Owned real-IAM/Loom repair handoffs](shuttle-avatar-admin-runtime-qa-2026-10-19.md)
now also pass in both directions, including a normal restart with measured
SIGTERM shutdown of0.672s. Telegram
and model responses were controlled fixtures, as described in that QA record.
The installed primary remains schema49/mvp56. The mvp68 candidate is built and
retained; it has not replaced that primary.

## Authorization and transactions

All routes require the normal Mini App origin, a current opaque ZinusyaSession
bearer, and server-verified zinusya.admin in this bot's organization. Public IDs,
participant eligibility, event authority and client-supplied role fields do not
grant global administrator access. Each response repeats the normal session/role
checks. The shared bounded positive IAM cache policy still applies.

Native writes use the same preparation, adoption and confirmation transactions
as agent tools. Lock order is operation advisory, actor SHARE, session, canonical
proposal FOR UPDATE, recipient SHARE and delivery UPDATE. Native session checks
reuse the already SHARE-locked actor. They do not call lockMiniAppBearer, which
would upgrade the actor to FOR UPDATE and can deadlock mutual administrator
repairs. Actor identity, bearer hash, active state, key generation, organization,
subject and database-clock session expiry are checked in the transaction.
The session is checked again before commit after any row-lock waits.

The HTTP handler captures local IAM invalidation generations before verifying
the role. The native transaction also checks that local fence and verified
credential expiry. An invalidation observed while confirmation waits aborts the
transaction, preserving the old delivery and receipt. This check does no network
I/O. Remote revocation that has not yet been observed still follows the normal
bounded cache policy; this is not an atomic transaction with Zitadel. Role or
session loss while returning a response can hide an already committed result.
After regaining access, reconcile the same operation; never infer non-delivery
from an authorization error or generate another avatar as a retry.

## HTTP contract

GET routes:

- `/app/avatar-admin/deliveries/{delivery}`: inspect the deployment-scoped delivery,
  including its captured recipient, current version, attempt and bounded receipt.
- `/app/avatar-admin/pending`: actor-owned canonical pending proposals. Optional
  `after` UUID and `limit` (default/max20). Follow `next_cursor`; pages also have
  the existing48KiB aggregate proposal budget.
- `/app/avatar-admin/proposals/{proposal}`: original actor-owned proposal, state
  and optional immutable completed result.
- `/app/avatar-admin/operations/{operation}`: the same view through a canonical
  or adopted operation ID. The returned operation_id is canonical.

POST `/app/avatar-admin/prepare` accepts a nonzero operation_id and arguments
containing delivery_id, positive expected_version, retry/dismiss action and
reason. Optional top-level proposal_id adopts that original pending proposal
under this operation. Omit it for a fresh proposal. confirmed=true is invalid
for preparation. The response is the original preparation document with
proposal_id, normalized arguments, delivery snapshot, description and expires_at.
The caller retains its operation ID; the operations route recovers its document
following response loss. Native preparation rechecks pending state and expiry
after locking the canonical row; completed/expired native preparation is rejected.

POST `/app/avatar-admin/confirm` requires operation_id, the exact proposal_id,
original normalized arguments and confirmed=true. It returns the immutable
resolution result. Neither a prior preparation nor discovery supplies consent.
Exact replay works across current sessions and interfaces, but still requires
current role and session authorization. After an uncertain response, read the
same proposal/operation before another explicit action. 400 means invalid input;
401 unavailable session;403 missing current administrator authority;404 missing
actor-owned/deployment record;409 changed, expired or completed preparation.
IAM/service unavailability uses the existing retryable503 path.

These routes reuse the existing canonical alias mechanism. A form-created
proposal can be adopted by the agent via resume_proposal_id and fresh exact broker
approval. A native caller can read an agent-created proposal and confirm its
canonical operation directly, or adopt it using prepare/proposal_id first.
Both directions commit one canonical result. See the
[shared resolution contract](shuttle-avatar-delivery-resolution.md).

## Reactive history and migration

Native prepare/confirm successes record minimal immutable metadata in the same
transaction: actor user, transport operation, proposal, kind, proposal snapshot version for preparation or immutable result version
for confirmation, and database time. Exact native replay does not move that history cutoff.
The next explicit user task receives at most five prior action locators, including
canonical operation and delivery IDs, newest first. No image, bearer, raw receipt
or private reason is copied into this context. Later proposal changes cannot
introduce a future confirmation into an earlier task's history.

History lookup is actor/deployment/task-owner scoped. A presence check returns
only that actor's history bit, then the task pipeline verifies current global
administrator authority before reading locators. Role loss withholds the context
without applying, replacing or cancelling proposals. Metadata is marked untrusted
history, never instructions or consent. The agent must read current authorized
proposal/delivery state and follow the explicit request. Native calls do not
start proactive tasks or Loom runs.

Migration59 adds only this action table/index. Existing proposals, results,
aliases and avatar bytes remain intact. Down uses NOWAIT on the action table and
its referenced user/canonical-proposal tables, so foreign-key removal cannot
silently wait on an active avatar writer before later guards. Any nonempty native
action history blocks rollback. Export and separately authorize deliberate
removal before reverting; no automatic discard/cascade is provided. Historical
avatar rollback tests include the new migration in dependency order.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
