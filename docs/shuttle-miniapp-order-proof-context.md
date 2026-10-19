# Native order proof acceptance history

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Migration43 adds a native acceptance journal for the
[owner proof form](shuttle-miniapp-order-proof-ui.md). The next explicit agent
turn receives bounded metadata about successful form submissions. This source
uses schema43 and the unchanged mvp-43 tool contract; no tool/API contract changed.
The owned main stand remains schema40/mvp-40. This source is not activated there.

## Atomic acceptance and replay

shuttle_miniapp_order_evidence_actions references the existing immutable submit
receipt by owner, operation UUID and fixed kind=submit. The table stores only
that reference and the first successful native acceptance timestamp. Original
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

Metadata contains operation/event/order/evidence IDs, original admission outcome
and UTC native acceptance time. It excludes money, file ID/locator/name/bytes,
review reason and credentials. The outcome comes from the original submission
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

## Verification and boundaries

The final Linux/PostgreSQL race gate passed68 roots /135 pass events:
22 integration roots /32 events,33 DB roots /61 events and13 task roots /42 events.
All 928 frozen inputs matched, with no failures/skips/OOM. Go/PG were bounded
at2GiB/512MiB; their exact containers and volume were removed and all ten main
service fingerprints remained unchanged. Retained proof is under ignored
.cache/shuttle-work/order-proof-context-v2-qa/.

Four new roots cover first explicit native acceptance of an agent receipt,
original pending outcome after human confirmation, exact retry/store reopen with
one unchanged timestamp and occupied allocation, task-time cutoff, bound5,
redaction, foreign task/deployment isolation, entitlement/module/user revocation,
atomic journal failure and same-key recovery, retained-row rollback refusal,
and document ingress -> native form acceptance -> restarted service -> next
explicit agent turn. The final sequence observes one upload agent run, no extra
run from the form, and exactly one more after the next Telegram message.

The first gate retains a failed await in its end-to-end test. The generic
paymentFile fixture called store.Intake with no queue callback, creating an
unrouted owner file task. The subsequent owner message waited for its predecessor;
this was test preparation, not a demonstrated journal/IAM defect. The corrected
case uses actual service.Accept document ingress and waits for that independent
upload turn before the form action. The v1 log, classification, source freeze and
abort cleanup remain preserved. The successful v2 did not overwrite that failure.

Initial DB/task/integration lint passed0 issues. After the test-only correction,
final integration lint also passed0; product DB/task source was unchanged. sqlc
compile passed and generated models include the new table. Initial correctness
and quality reviews and a fresh final source review found no actionable defects.
No changes were staged or published.

These tests use real Go/PostgreSQL and mock IAM/Loom/Telegram peers. They do not
prove real Telegram Mini App activation, real Zitadel/Loom interaction, unattended
model behavior, actual transport disconnection or hard-kill recovery. Earlier UI
and reviewer context gates retain their own acceptance boundaries. Next: authorized
durable notices, owned image/migration activation, then real native/agent/mixed
and restart/fault acceptance. D23 financial-policy questions remain unanswered;
this journal introduces no new money policy. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
