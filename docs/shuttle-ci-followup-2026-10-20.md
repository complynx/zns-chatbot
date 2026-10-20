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
two lock tests passed; the three avatar tests returned503 during initial
draft/generation setup. The authentication-labelled response is also used for
other avatar errors and does not identify an IAM failure. Those failures do not establish the cause of the original CI
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

## Later CI and deterministic fixture capacity

The [later run at e22dca0a](https://github.com/complynx/zns-chatbot/actions/runs/39447251353)
also failed after all nine batches: 808 of814 integration roots passed. The two
lock-observation roots failed again. Four avatar roots returned503, including
`TestAvatarFinalizationAPIReplaySkipsRendererAfterLaterDraftEdit`. Response bodies
are retained under ignored `ci-critical-e22dca-v1/`. Later workflow steps were
skipped. This run does not establish a renderer or IAM root cause.

The pinned pgx5.11.0 pool default is `max(4, runtime.NumCPU())`. The payment-review
fixture holds a blocker, two financial transactions and logout, then needs an
additional connection to observe locks. A runner exposing at most four CPUs gets a default of4. Docker CPU quota
alone does not pin this fixture capacity.

A paired Linux/race probe freezes one shared source manifest and selects the
same five roots with explicit pool limits4 and8. With4, both original lock roots
fail and all three selected avatar roots pass. With8, all five pass. Neither case
has skips or OOM; both source manifests match, dumps decode, resources are
removed and the primary is preserved. Evidence: ignored
`ci-critical-pool-matrix-result-v1.json` and both `ci-critical-pool-*-pg-v1/` roots.

The integration harness now defaults to8 connections when the DSN has no
explicit `pool_max_conns`; explicit limits remain unchanged. Production settings
are untouched. A fresh no-override gate passes nine roots and13 cases; strict
lint reports zero issues and source/controller review is clean. The original
four avatar CI failures still need investigation and a new full CI pass.

## Avatar integration process boundary

The avatar HTTP fixtures previously called `renderer.Render` inside the
race-instrumented test process. Production uses the separate `zinusya-avatar`
executable. Both paths are subject to the existing three-second request render
budget, so the fixtures included race instrumentation in a budget intended for
the production worker. This is a test/deployment boundary mismatch; the retained
CI response bodies alone do not prove that every503 was a render timeout.

The affected HTTP fixtures now build the actual leaf executable before requests
and use `avatar.NewExecutable`. Only that child is built without race
instrumentation, with CGO disabled. The HTTP server and database integration
remain under `-race`. Each fixture owns its temporary executable; Go's build
cache amortizes compilation. The processor still passes the request context,
uses its production process gate, sanitizes the child environment and retains
both production deadlines. Expected image bytes computed outside requests and
all replay, current-session, conflict and wrong-provenance assertions remain.
Production code and timeouts are unchanged.

A fresh bounded Linux/PostgreSQL race gate passes all24 roots and30 cases from
the four affected avatar test files, with no skips or OOM. The source snapshot,
logs and fully decoded database dump are retained under ignored
`ci-avatar-worker-pg-v1/`; its containers, network and volume are removed and the
primary is preserved. A later formatting-only change wraps the helper's build
command; strict lint then reports zero issues.

A separate diagnostic selects exactly the four avatar roots that failed the
later CI and limits the test container, including its worker, to0.5 CPU. All four
pass with no skips or OOM. The build happens before timed HTTP requests; cold
compilation time is part of total test duration. Its logs, frozen source and
fully decoded dump are retained under ignored
`ci-avatar-worker-pressure-pg-v1/`. Its owned containers, network and volume are
removed; the primary is preserved. This is a selected CPU-pressure diagnostic,
not full CI or an exact reproduction of the hosted runner. A new exact-source
remote CI pass remains required. Primary remains mvp81/schema65.

## Full behavioral pass and dependency-download failure

The [run at1deb3cc2](https://github.com/complynx/zns-chatbot/actions/runs/39456420165)
passes all814 integration roots in nine batches, the package/race tests,
stand isolation, Telegram simulator, alert/dashboard checks and all344 UI tests.
It then fails while building pinned developer tools: Go proxy returns an HTTP/2
`INTERNAL_ERROR` fetching a `google.golang.org/genproto/googleapis/api` archive.
Generated SQL, lint and final Linux/container build are skipped. This run is a
failed full CI result despite its passing behavioral tests. The primary remains
mvp81/schema65 and the installation gate stays closed.

The two reviewed documentation commits were pushed after that terminal result.
The [new run at83e8701d](https://github.com/complynx/zns-chatbot/actions/runs/39463177833)
is pending full acceptance; no product, integration-test or workflow source changed
between these two heads. Its push counterpart was cancelled by configured
concurrency, which is separate from a test failure.

## Following run exceeds the available job budget

The [run at83e8701d](https://github.com/complynx/zns-chatbot/actions/runs/39463177833)
is now terminal/cancelled, not accepted. All814 integration roots and344 UI cases
pass again. Tool installation and generated SQL checks complete successfully.
Lint prints `0 issues.` but its step is marked cancelled, and Linux/container
build is skipped. Do not count that lint step as successful CI.

The job starts at17:06:41 UTC and completes at17:51:46 UTC, against a configured
45-minute limit. Cancellation occurs at the budget boundary; no newer branch
workflow run was observed. An explicit timeout annotation was unavailable, so
budget exhaustion is a timing-supported diagnosis rather than a distinct
reported `timed_out` conclusion. The measured preceding steps leave no time for
the required build under that budget.

Increase only the overall job limit to60 minutes. Existing ten-minute package and
integration-batch limits, test selection, race detector, checks and failure
handling remain unchanged. A fresh full remote pass is required; neither this
cancelled run nor local component results open the primary installation gate.

## Following run exposes a revocation-status assertion

The [run atd625be69](https://github.com/complynx/zns-chatbot/actions/runs/39470207733)
finishes with failure:813 of814 integration roots pass. The remaining root is
`TestUnknownFileSizeUsesCloudRefusalWithoutExposingForeignMetadata`. Its direct
content GET after local revocation returns401 where the test requires403. Later
UI, tool, SQL, lint and build steps are skipped; this is not full CI acceptance.
The original log and previous failed canonical receipt remain archived locally.

The implementation can reject a verification that overlaps cache invalidation.
A controlled local IAM probe holds the first introspection response across
`Invalidate`: that `Verify` returns `ErrDenied`, and a fresh verification of the
same still-valid synthetic token succeeds. The API maps identity denial to401;
a request that reaches the revoked local-account check instead receives403.
This proves the permitted outcome, not the exact interleaving in the failed CI.
The temporary probe source and log are retained; its owned worktree file is removed.

Only the two post-revocation file assertions now accept401 or403. Foreign-owner404,
known/cloud oversize413, unavailable503 and exact Telegram read counts remain
strict. Production authorization, invalidation and file-reading code are unchanged.
The bounded Linux/PostgreSQL/race follow-up passes all three existing file-limit
roots five times each:15 executions, no failures or skips, exit0 and no OOM.
It uses frozen source, keeps exact Telegram read-count assertions and has clean
independent reviews of the test change and QA controller. Remaining fixture state
is archived and fully decoded; both owned containers, volume and network are
removed. The source and primary stand remain preserved. This is not the full
integration suite or a restored-database test. A fresh full remote pass is still
required before the prepared primary installation. Local follow-up evidence is in
`.cache/shuttle-work/ci-file-denial-pg-v1/`; controlled IAM proof is in
`.cache/shuttle-work/ci-auth-denial-race-v1/`.
