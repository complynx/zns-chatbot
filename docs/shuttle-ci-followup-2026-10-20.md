# CI follow-up, 2026-10-20

The [CI run at 1f0d1150](https://github.com/complynx/zns-chatbot/actions/runs/39437346623)
failed. All nine integration batches ran: 809 of 814 discovered roots passed.
The preceding cmd/internal/migrations race checks passed. Later workflow gates
were skipped; this is not a full CI pass.

The five failing roots were:

- `TestAvatarFinalizationAPIConcurrentNativeAgentCreatesOneDocument`: HTTP 503.
- `TestAvatarTransformKeepsIdentityAndReplaysCanonicalEditedRequest`: HTTP 503.
- `TestAvatarPreviewNativeAgentDescriptorAndPinnedFrameAgree`: HTTP 503.
- `TestDetailsRequestBeginAndOwnAdminEventConfirmationDoNotDeadlock`: lock observation timed out.
- `TestMiniAppPaymentReviewConcurrentEventsAndLogout`: lock observation timed out.

An unchanged-source Linux/PostgreSQL race probe passed all five roots with no
skips or OOM. A separate diagnostic limited the test process to 0.5 CPU. Its
two lock tests passed; the three avatar tests failed earlier, during authentication
or initial setup. Those failures do not establish the cause of the original CI
503 responses. Both probes retain their original logs and decoded database dumps;
their owned containers, networks and volumes were removed. The primary was preserved.

The test correction gives every integration fixture its own PostgreSQL
`application_name`, using its existing unique schema name. The payment-review
lock observer now excludes connections from other fixtures in the same database.
Previously its global counts could include parallel tests. Avatar assertions now
include the response body when a request fails. Production code, rendering and
timeouts are unchanged. The other original CI failures remain unconfirmed.

The focused follow-up selects the five original roots plus all four other
Mini App payment-review roots. It passes all nine roots and 13 cases with no skips or OOM. Strict lint
reports zero issues, and the source/controller review has no actionable findings.
A new full remote CI pass is still required before installing mvp83 on the primary, which remains
mvp81/schema65. No production-readiness claim is made.
