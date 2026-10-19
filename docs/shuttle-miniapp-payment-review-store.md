# Session-bound native payment review store

The native transaction store is implemented and component-verified. The
[HTTP adapter and proof access](shuttle-miniapp-payment-review-http.md) now also pass
component checks. The [local launch rollout](shuttle-miniapp-local-launch-qa-2026-10-16.md)
installed this store at schema39 / manifest mvp-39. The [reviewer form](shuttle-miniapp-payment-review-form.md) passes source component
checks. Its installation, native entry, reactive agent context and full real-service
review acceptance remain open.

## Shared financial boundary

PrepareMiniAppPaymentReviewWorkspace saves an exact intent without crediting money;
CancelMiniAppPaymentReviewWorkspace cancels only that pending intention. They use
the same implementations as the agent entry points. Each saved edit is a new
immutable revision, while retries return the original authorized receipt.

CommitMiniAppPaymentReviewWorkspace takes full normalized review parameters,
workspace/revision, canonical proposal P and a stable operation L. It checks the
session and exact decision, creates a distinct outer proposal/alias, applies the
existing canonical review, and completes the native receipt in one transaction.
Ledger, balance version, owner notification intent, workspace and receipts roll
back together. A previously completed canonical decision is recovered before
new-effect expiry/current-revision checks. It never credits money twice.

The native request hash includes P as well as the exact review. Source is set by
the server wrapper. A write cannot claim the other transport's operation or change
its provenance. Authorized operation/status reads may recover either transport's
result. Native calls persist no incomplete commit alias. Existing source=miniapp
completed call rows form the action journal for save/edit/cancel/confirm, but their
redacted projection into the next explicit agent turn remains unimplemented.

## Session and locks

IAM verification must precede these store methods. The store then checks session
ownership, organization/subject, bearer hash, key generation, active state, local
revocation and database-clock expiry in the mutation transaction, including on
matching retries. Native operation status also checks the session before disclosure.

Lock order remains outer operation -> canonical operation -> event UPDATE -> actor
SHARE -> session UPDATE -> target SHARE. The payment guard does not call the existing
owner-UPDATE Mini App guard: upgrading actor SHARE would risk deadlock for concurrent
reviews of different events. Logout takes owner UPDATE before session UPDATE, so it
waits for financial transactions already holding actor SHARE.

## Component proof and limits

The exact pre-change source matched the prior API gate's 831-input freeze. The new
broad gate passed 56 top-level tests / 82 passing test events with Linux Go race,
real disposable PostgreSQL and mock external peers. Its 833-input freeze remained
unchanged. This includes existing payment, notification and authority behavior.

After that gate, only the new concurrency test was strengthened to run logout
while two different-event reviews wait on their shared session. The final focused
gate passed all five native roots / nine events, with the same 833-path input set
and an unchanged final source freeze. Other selected packages compiled but had no
matching tests in that focused run. No full 56-root rerun is claimed after the
test-only edit. Scoped db/integration lint reports zero issues.

The five roots cover native-to-agent and agent-to-native decisions; canonical
receipt recovery after expiry/reopening the store; mismatched proposal/amount and
source collisions; cancellation/edit plus historical receipt; logout, expired session, key-generation change
and foreign owner denial; rollback after the financial mutation before
outer completion; and concurrent reviews with actual logout waiting. No production
or real IAM/Loom/Telegram workflow acceptance is inferred from these tests.

Three fresh source reviews found no actionable defects: normal and quality reviews,
then a complete final review after the helper/test updates. Initial lint failures
are retained; no test failure occurred. Both bounded PostgreSQL/Go pairs and their
volumes were removed after verification, with terminal zero/no OOM. Main ten
container identities, images, starts and restart counters stayed unchanged.

Local proof files: .cache/shuttle-work/payment-native-store-qa/checkpoint.json and
.cache/shuttle-work/payment-native-store-final-qa/checkpoint.json; final lint.txt
and expected-roots.json are in the latter directory. These are local QA artifacts,
not release or production evidence. The index remains empty; D04 blocks publication.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
