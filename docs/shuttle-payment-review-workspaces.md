# Shared payment review workspaces

This is the typed store layer for the [native reviewer plan](shuttle-native-payment-review-plan.md).
The [agent HTTP adapter](shuttle-payment-review-workspace-api.md) now passes component checks.
The [native session store](shuttle-miniapp-payment-review-store.md) now passes component checks;
the [HTTP/proof adapter](shuttle-miniapp-payment-review-http.md) also passes component checks.
The form and complete mixed runtime workflow remain open.

## Exact decisions and identities

PreparePaymentReviewWorkspace saves a normalized review and an expected-revision CAS.
For one actor/evidence, each edit creates a new immutable workspace ID and increasing
revision. A workspace's ID is also its canonical payment operation C; its preparation
contains the existing five-minute canonical proposal P. Creating a workspace reserves
and credits no money. Cancellation changes only the pending intention.

PreparePaymentReviewCommit binds a separate transport operation L to that exact
workspace/revision and normalized review. Its outer proposal is distinct from P and
expires at the original canonical deadline; retries do not renew it. Commit arguments
include the actual decision, amount, reason and balance version. An ID alone cannot
silently select changed money parameters. The agent API requires explicit
exact-parameters approval. The native form must require an explicit confirmation;
that adapter is not implemented yet.

CommitPaymentReviewWorkspace returns the canonical operation ID and original review.
The agent API returns L in its transport envelope, as required by Loom. Multiple
approved aliases of the same C share one financial effect and owner notification.
Different reviewers retain independent drafts and the existing balance/evidence guards.

## Access and atomicity

PaymentScope comes from verified identity, including a current verified global role;
actor, organization and rights must never come from request arguments. The store checks
current local identity, event module and event authority under the existing event lock.
Workspaces and transport receipts belong to one actor. Knowing another actor's workspace
or operation ID grants no access.

The canonical mutation boundary also checks whether an associated workspace remains
prepared and current. Thus the existing review entry cannot execute a cancelled or
superseded workspace proposal. A completed canonical receipt is recovered before these
new-effect guards and expiry, preserving historical replay. A successful canonical
mutation also completes its workspace in the same transaction.

Financial review, ledger version, owner notification intent, canonical receipt,
workspace completion and executed outer receipt share one transaction. A receipt-write
failure rolls all of them back. Writes acquire the outer call lock, then the canonical
operation, event and actor locks. The existing domain entry uses its canonical/event
lock order. No remote IO occurs under these locks.

## Recovery and reads

PaymentReviewWorkspace reads an exact revision; CurrentPaymentReviewWorkspace discovers
this actor's latest revision for an event/evidence. Operation replays return original
snapshots. Their Current/state fields describe the original snapshot; use a fresh read
for current workflow state. Prepared state does not imply the proposal is unexpired,
the evidence still pending or the balance unchanged. Execution rechecks all of these.

PaymentReviewWorkspaceOperation takes only L and checks current access.
A pending commit
returns the workspace and original outer preparation, allowing recovery after a lost
prepare response.
It also checks for the bound canonical receipt.
It does not write an outer receipt or imply that L
itself executed.
A later commit of L can persist its alias receipt without new money.

Pending operation reads also expose unavailable_reason for cancellation, supersession
or expiry, using database time and locked canonical preconditions.

A successful prepare/cancel receipt has Kind=prepare/cancel and no review result;
Completed alone must never be interpreted as paid. A missing operation is unknown,
not proof of non-execution. For a pending commit, consumers must inspect workspace
current/state and original expiry; a cancelled, replaced or expired intent does not
authorize a new operation without a new explicit decision.

New proposal instants and database-decoded outer expiries use UTC for stable replay.
The existing canonical description continues to show event-local IANA deadlines.

## Integration still required

No proactive invocation.
- Localized exact money presentation, full mixed-path tests and owned real-service QA.

Agent entry points retain agent provenance; native wrappers atomically check the session
and record miniapp provenance on completed calls. Native journal context and browser
exposure still require their adapters. Evidence file access, policy and notification delivery behavior otherwise
remain owned by the existing payment implementation.
