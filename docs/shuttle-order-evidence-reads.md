# Order evidence reads and receipt recovery

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

This store increment builds on [order evidence admission and review](shuttle-order-evidence-store.md).
It does not add HTTP tools, Mini App controls, notice delivery or a new live deployment.
At this store gate, source was schema 41 / manifest mvp-41. The subsequent
[read-only agent API](shuttle-order-evidence-api.md) advances source to manifest
mvp-42. The owned bot remains schema 40 / manifest mvp-40.

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

## Verification and limits

The isolated PostgreSQL/Linux/race run in the ignored local directory
`.cache/shuttle-work/order-evidence-reads-v1-qa/` passed 15 top-level scenarios,
with 22 pass events, no failures, no skips and no OOM. The frozen source inputs
matched before and after execution. Tests cover original receipts across a reopened
store, current evidence after review, actor and phase isolation, role/owner revocation,
proof locator isolation, deployment scoping and orders-only versus disabled modules.
The 12 existing admission/review scenarios ran alongside the 3 new read scenarios.
A reopened store is not an application kill/restart or Telegram UI acceptance.

Two independent source reviews returned no actionable findings. The final current
diff was reviewed after a lint-only switch rewrite. Scoped db/integration lint passed.
The initial formatter invocation with empty stdin failed before changing any file;
the subsequent per-file formatter completed normally. No functional test failed.
Only this run's two containers and its labeled PostgreSQL volume were removed.
Fingerprints of all ten main stand services remained unchanged.

The subsequent API increment adds bounded owner history, reviewer queue, discovery,
files and receipt lookup. Next: shared revision-bound review workspaces, native owner proof targeting,
notice delivery and native/agent/mixed real Telegram acceptance. Do not expose an
independent review mutation that bypasses the shared pending revision.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
