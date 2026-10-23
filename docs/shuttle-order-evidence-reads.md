# Order evidence reads and receipt recovery

## Current evidence and original receipts

`OwnOrderEvidence` reads the owner's current evidence state. `OrderEvidenceForReview`
reads current evidence for a current event ambassador, event administrator or verified
organization administrator. Both use the event's `orders` module. Reviewer access
requires no `registrations` module and no pass for the reviewer. A revoked payer's
historical claim remains available to a current reviewer for inspection and resolution.
A revoked payer cannot use owner reads.

`OrderEvidenceOperationByID` accepts an operation UUID and explicit `submit` or
`review` phase. It returns only the caller's original immutable receipt after checking
current access. Submission receipt recovery requires current owner entitlement;
review receipt recovery requires current event authority. The first transaction
resolves the receipt's event under the actor lock, releases that transaction, then
rereads the receipt under normal event-to-actor authorization. It does not trust an
event hint from the caller. An absent receipt is unknown, not proof of non-execution.

A submission receipt can therefore still report `pending` after a reviewer has
confirmed the claim. Use a current evidence read for the subsequent decision.
Recovery does not create a new claim, acquire capacity or repeat a human decision.

## File access

`OrderEvidenceFile` uses an evidence UUID as its external locator. It does not accept
a foreign file UUID. The explicit owner path requires ownership and current assigned
pass entitlement. The explicit reviewer path requires current orders authority,
including when the reviewer also owns the proof. The file must belong to the
associated evidence owner in the same bot and organization.

The store returns metadata and the private Telegram locator to its server caller.
It does not download bytes or grant a reusable public URL. Server consumers
must authorize again after Telegram IO and before returning a descriptor or content.
The subsequent API increment implements the HTTP rechecks and source identity comparison.
Generic owner file access is unchanged.
