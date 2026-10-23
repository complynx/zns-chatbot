# Native order proof acceptance history

The next explicit agent
turn receives bounded metadata about successful form submissions.
This source
uses schema43 and the unchanged mvp-43 tool contract; no tool/API contract changed.
This source is not activated there.

## Atomic acceptance and replay

shuttle_miniapp_order_evidence_actions references the existing immutable submit
receipt by owner, operation UUID and fixed kind=submit.
Original
money, reasons, file provenance and receipt results are not copied or rewritten.

SubmitMiniAppOrderEvidence inserts the journal record and commits in the same
transaction as admission, holds, original receipt and retained notices. A journal
failure rolls back a new admission completely. Exact retry uses the original
operation/hash/result and ON CONFLICT preserves the first journal timestamp.
Current native session and owner entitlement checks still apply to replay.

An earlier agent receipt is not automatically labeled native. The first explicit
native retry of that exact operation can add a journal row at its current native
acceptance time. This says the native request was accepted then; it does not
claim the old submission originally came from the form or confirm money again.
No old receipts are bulk-backfilled. Journal insertion does not invoke Loom.

## Context and access

The existing order context builder reads at most5 newest native acceptances before
that task's creation time, ordered by completion time and operation UUID. It
validates the current owner bot/organization/subject/nonrevocation, task ownership,
orders module and assigned registration with a pass. An unpaid assigned pass is
sufficient. A foreign task or lost module/entitlement cannot disclose that history.

It excludes money, file ID/locator/name/bytes,
review reason and credentials.
The outcome comes from the original submission
receipt, not a later ambassador decision: a pending submission receipt can remain
pending history after the current proof becomes confirmed.

The prompt labels history untrusted and denies treating it as instructions, new
consent or a current request. It directs the agent to
zinusya_order_payments.operation with kind=submit for the original receipt and
evidence for current authorized state. No proactive agent task is created.
Existing persisted requests stay frozen. Uploaded documents are independent
user-triggered turns; form submission adds no new agent run. The next explicit
message may continue the form action.

## Migration and rollback

Apply additive migration43 before starting this source's workers. Existing
receipt rows and agent writer behavior are preserved; older writers simply do
not add native journal rows. Keep the new table on binary rollback so retained
history survives. Down migration locks the journal and refuses to drop it while
rows exist; export/retention and any clearing require an explicit operator action.
No destructive contraction or binary rollback was performed by this component
acceptance. Full deployed migration/rollback and mixed-binary QA remain separate.
