# Shared order review workspaces

Store checkpoint: schema 42 / manifest mvp-42.
The subsequent agent mutation checkpoint used manifest mvp-43;
[agent mutations and complete consent](shuttle-order-evidence-mutations.md) are
verified separately.
The owned main was read
back as schema 40 / manifest mvp-40.
This extends the
[order evidence read API](shuttle-order-evidence-api.md) and
[evidence business transaction](shuttle-order-evidence-store.md).

## One decision, several transports

An actor owns monotonically numbered immutable revisions for an evidence UUID.
Saving a decision requires expected_revision, with 0 for the first save. Each revision
has its own workspace UUID, exact normalized review arguments, canonical proposal,
expiry and prepared/cancelled/completed state. A later revision supersedes the
old one without changing its stored decision. Current reads derive whether a
revision is latest. Saved-intent receipts retain the original snapshot; their
historical current flag must not replace a current workspace read.

The agent can save or read a decision begun in the form, and the native store
entry points can continue one begun by the agent. Native confirmation repeats the
workspace UUID, revision, exact review and displayed canonical proposal. Agent
confirmation first obtains a distinct transport proposal bound to the exact same
workspace/revision/review. Transport operation UUIDs are actor-owned and source-bound;
one source cannot reuse another source's write operation. Current authorized reads
can recover that actor's receipts from either source.

Saving, replacing or cancelling a review is intent only. Cancelling a review does
not reject the proof, release its capacity or refund money. Superseded, cancelled
or expired intent cannot start a decision. Preparing an agent commit also checks
that the evidence still accepts that decision. Existing currency/admission policy
is unchanged: confirmation only supports the exact timely full-price admitted
claim. Unsupported partial/mixed/late cases stay needs_decision and may be rejected
with a human reason, without implying a refund.

## Atomic execution and recovery

The workspace UUID is the canonical decision operation. One caller-owned transaction
writes the human decision, this attempt's held-to-occupied/released allocations,
business notices, canonical receipt, workspace completion and outer source receipt.
No independent commit occurs inside the shared preparation/review helper. Native
outer aliases are created inside that same transaction; failure rolls them back.

Recovery checks current authority first, then an existing source/canonical receipt
before freshness checks. A completed result can therefore be recovered after the
original consent expires. Without a completed canonical receipt, old consent cannot
cause a new effect. Concurrent source aliases return the same decision; competing
reviewers can commit only one decision against the evidence revision.

OrderReviewReceipt.Completed describes its phase. A completed save/cancel receipt
is not confirmed money; only a commit receipt with the actual review result records
a decision. Missing receipt means unknown, never proof of non-execution. Current
workspace/evidence reads remain separate from original immutable receipts.

Every store path uses current orders-event authority and exact deployment identity.
No registrations module or reviewer pass is required. Native session generation is
checked inside the business transaction after event/actor lock waits, including
receipt replay. A revoked payer's historical proof can still be rejected by a
current reviewer; revoked reviewer/session access is denied. No network IO occurs
inside these transactions.

Standalone exported preparation/review entry points remain compatible for proofs
where that actor has never created a shared workspace. Once shared history exists
for that actor/evidence/event, those standalone writers return conflict. They
cannot consume the canonical proposal, an older unrelated standalone preparation
or a newly created independent intent to bypass revised/cancelled shared consent.
Shared execution uses the private transaction-local helper.

Schema 42 adds dedicated workspace/call tables; it does not rewrite financial
history. Its downgrade refuses to remove nonempty workspace or receipt history.
This is not proof of a live schema upgrade, downgrade or application restart.
