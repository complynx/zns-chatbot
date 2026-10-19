# Native order reviewer writes

Current owned runtime is schema49 / `mvp-56`; the
[latest owned image gate](shuttle-session-fallback-primary-qa-2026-10-18.md)
preserves the existing owner/reviewer forms, history and durable notices.
[Native proof/review](shuttle-order-proof-native-qa-2026-10-17.md),
[native-to-agent continuation](shuttle-order-review-late-real-qa-2026-10-18.md),
[owner mixed workflows](shuttle-orders-owner-real-qa-2026-10-18.md) and
[pure-agent review/cancellation](shuttle-order-reviewer-dialogue-qa-2026-10-18.md)
have scoped real Telegram/IAM/Loom proof with a manual provider.
The [isolated browser handoff](shuttle-order-browser-handoff-qa-2026-10-18.md)
now also passes the original blocked agent commit→native form→same-agent flow,
with distinct customer/ambassador identities and one financial review. It uses
mock Telegram and the actual shipped form/backend; the earlier real WebView input
barrier remains separate. Fault/outage, unattended model and D23 money-policy
questions remain open. Component versions below are historical milestones.

Source schema 42 / manifest mvp-43. These routes complement the
[reviewer reads](shuttle-miniapp-order-review-reads.md) and use the existing
[shared review workspace](shuttle-order-review-workspaces.md). No store, schema,
manifest or financial policy change was added at that milestone. Current owned
runtime and its scoped real-service acceptance are linked above.

## Requests

All three POST routes use the existing Mini App bearer session, server-side IAM
verification and orders event authority. A reviewer does not need a participant
pass or the registrations module. Identity, deployment and global-admin status
come from the verified session/principal, never from request JSON. Existing
same-origin checks, strict JSON decoding and the 64 KiB request bound apply.
Unknown fields and trailing JSON are rejected. Responses use the existing bounded
reviewer result adapter and recheck the current Mini App session before delivery.

POST /app/order-reviews/workspaces requires nonzero operation_id, explicit
expected_revision and a review object. expected_revision is zero for the first
saved decision, then the latest actor/evidence workspace revision. Optional
language is auto, en or ru; omission follows existing language resolution.
review contains event_id, evidence_id, explicit expected_version, decision and
reason. The current unreviewed evidence revision is 1. decision is confirmed or
rejected; a nonempty reason is required for either choice, trimmed and limited
to 512 Unicode code points. Saving prepares the exact decision and its immutable
full summary; it does not confirm money or move holds.

POST /app/order-reviews/cancellations requires nonzero operation_id, workspace_id
and positive revision. It cancels that exact current saved decision. It does not
reject the underlying evidence, release its holds, cancel an order or perform a
refund. Retrying the original operation returns its original cancellation receipt.

POST /app/order-reviews/confirmations requires nonzero operation_id,
canonical_proposal_id, confirmed=true and arguments. arguments repeats workspace_id,
positive revision and the full review object, including explicit expected_version.
The proposal is the saved workspace preparation's proposal_id. Changing the reason,
decision, target, revision or proposal cannot reuse earlier consent.

The form supplies no credited-amount override. The shared order domain derives
confirmed money from the evidence and decision. Only pending admitted evidence
can be confirmed; rejected decisions credit zero and release that claim's holds.
Unsupported/exhausted cases do not gain a confirmation bypass through this API.
No new partial-payment, late-admission, mixed-capacity or refund policy is chosen.

## Common intent and recovery

All writes use PrepareMiniAppOrderReviewWorkspace, CancelMiniAppOrderReviewWorkspace
or CommitMiniAppOrderReviewWorkspace. They never call standalone raw review
primitives. Agent and form share the same actor/evidence revision and canonical
business result, while source-specific operation receipts retain their provenance.
A transport cannot silently reuse the other source's operation ID for a write.

Native confirmation binds the canonical proposal. Agent confirmation still uses
its own transport proposal from PrepareOrderReviewCommit. Both exact paths can
recover the same canonical decision without a second review, allocation or notice.
Current event/actor/session authorization runs inside the transaction after lock
waits, including for completed-result replay. A logout or role loss while a request
waits prevents new effects and an apparent successful replay under stale rights.

Keep the exact operation/body before sending a mutation. On an uncertain response,
read GET /app/order-reviews/operations/{operation} after signing in if required.
404 means unknown, not that nothing happened. Retry the same operation and body;
a new key is not a recovery strategy. Existing operation/kind/source/arguments
checks return conflict for changed intent. Canonical completion is recovered before
new-effect expiry checks, after current authorization. A saved historical receipt
and the current workspace/evidence are different reads.

The [reviewer UI](shuttle-miniapp-order-review-form.md) shows the full immutable
preparation description and requires explicit confirmation. Edits create a new revision; old, superseded or
cancelled consent cannot execute. Loading an existing workspace or reading its
receipt does not itself grant consent or start Loom. Reactive reviewer context
has separate source verification; [owner proof history](shuttle-miniapp-order-proof-context.md)
also has separate source verification. Authorized durable notice delivery now has separate
[source verification](shuttle-order-notice-delivery.md). Activation was pending at
that original component milestone; current installed state is linked above.

## Verification and limits

The focused Linux/PostgreSQL race gate passed 36 roots / 85 pass events:
23 integration roots / 39 events and 13 API roots / 46 events. It covered native
review reads/writes, shared workspace rules and the existing agent mutation API.
No failures, skips or OOM occurred; all 921 frozen inputs matched before and after.
Scoped API/integration lint passed with zero issues. Two initial independent source
reviews and a fresh final review found no actionable defects in the write adapter.
The shared store also has its earlier independent review and focused proofs.

Four added HTTP behavioral roots cover both agent/form handoffs, exact retries,
original save/cancel receipts, stale edits/cancellation, explicit consent/version
fields, unknown role injection, foreign Origin, ordinary-owner denial, altered
canonical proposal/decision, and logout/role loss while confirmation waits on an
event lock. Recovery tests discard a successful response body, sign in with new
signed Telegram initData, read the stored receipt and reopen the store. They do
not simulate a transport disconnect or a hard process kill; those remain separate
fault-matrix acceptance. The broader shared-store tests include canonical replay
after completion/expiry and rollback/concurrency cases.

Proof artifacts are in ignored .cache/shuttle-work/native-order-review-write-v2-qa/.
Its exact containers and volume were removed; the ten main services were unchanged.
The first failed snapshot remains in the v1 directory: same-second test initData
was reused with a new nonce, so authentication correctly rejected a purported new
launch after the decision had committed. The fixture now signs a new query_id;
no authentication policy was relaxed. Initial lint findings about unnecessary
serial subtest wrappers also remain recorded.

These tests use real Go HTTP/store code and disposable PostgreSQL with test IAM /
Telegram peers. They are not real Zitadel/Loom/Telegram manual acceptance. Native
reviewer UI now has separate source/browser fixture verification.
[Reviewer action metadata](shuttle-miniapp-order-review-context.md) also has separate
source verification. [Owner proof history](shuttle-miniapp-order-proof-context.md)
also has separate source verification. At that original write-component milestone,
notice delivery, activation and real-service/fault QA were pending. Current
installed state and scoped native/fresh mixed acceptance are linked at the top.
Remaining gates do not imply production readiness or publication.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
