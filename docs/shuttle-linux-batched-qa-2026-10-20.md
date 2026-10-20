# Bounded Linux verification and UI follow-up, 2026-10-20

Status: focused follow-up accepted; complete current-source repetition remains
open. The preceding eight-batch run is failed and retained as failed. No
production service or Loom developer stand was changed. The owned primary
remains schema64/mvp78 with its preceding binary and assets, eleven services
and134 Loom runs; these UI changes are not installed there yet.

## Eight-batch result

The new [integration runner](shuttle-integration-batches.md) executed785
Linux/default root tests in eight sequential processes, at most100 roots and
two parallel tests per process. It used Go1.27.2, the race detector and a fresh
owned PostgreSQL17 database. The Go/Python QA image and compiler are test-only.

784 roots passed; one root failed in its `amount` subcase. There were no skipped
tests, OOM or captured race warnings. The captured Go events span1509.55 seconds;
that excludes initial discovery/build and later archival cleanup. This measurement
also leaves little room in the old30-minute CI job for setup, lint and builds.
The job budget is45 minutes; each integration process remains limited to10 minutes.
This is a job allowance, not a relaxed application or business deadline.

The failing test rejected any Telegram message to the reviewer. Its fixture had
also submitted payment proof. The retained database shows the normal40.00-BYN
pending-proof notice delivered to that reviewer and an empty task-specific
outbox. The failure did not prove unverified consent or a payment mutation. The corrected
behavior assertion permits ordinary notifications while rejecting the stored
consent heading, untrusted provider text and approval keyboard. It copies sent
messages before asserting, so a failed assertion cannot retain the peer mutex.

Go discovery selected785 roots rather than the787 raw declarations. One test
intentionally requires the separate `browserqa` build tag. Another was in
`meal_draft_windows_test.go`: Go treated the business word "windows" as a
Windows-only filename suffix. Its exact-content rename to
`meal_draft_deadlines_test.go` enables it on Linux. The current default Linux
set therefore has786 roots. The ignored first controller also assumed787;
that extra guard failed closed. Its result was independently failed by the
actual assertion. The shipped runner uses Go discovery and exact selected-root
coverage rather than this static declaration count.

## Verified follow-up

A fresh isolated Linux/race run passes all four selected roots and11 cases:

- Payment consent uses original stored human terms in English and Russian.
- Changed amount, excessive approval expiry and revoked authority cannot offer
  unverified payment consent; confirmed money remains zero.
- Completed native and agent decisions reconcile their original result.
- Meal draft edits preserve closed lunch choices, permit open dinner changes,
  reject erasing closed items and honor an authorized deadline extension.

The last test is now actually executed on Linux. This focused gate exits0 with
no skips or OOM. It is not a new full786-root run. The earlier full-suite failures
remain in the [preceding report](shuttle-linux-race-qa-2026-10-20.md).

The source also closes two findings from the checkpoint code review. Deselecting
a pass clears its active payment form and loaded file. Paging preserves the
active pass as a visibly selected option. Legacy flat-price pair proposals and
receipts without quotes can be inspected and continued natively using their
immutable description. Both recipients remain recognizable by name and Telegram
ID, including recovery outside the current list page, with fresh explicit
consent. Current structured proposals still require two valid quotes.

All316 Mini App/order behavior cases and37 API/task unit roots pass against the
applied source, with no skips in those Go packages. The new runner has four
focused acceptance checks and a passing missing-database CLI guard. Independent
correctness, quality and usability review found the two UI defects; the fixes
then passed two review rounds. The focused test/rename/controller follow-up has
clean independent correctness and quality reviews. Go payment and authorization
business logic was not changed by this follow-up.

## Evidence and remaining work

Ignored broad evidence is in `.cache/shuttle-work/linux-race-batched-v1/`:
per-batch JSON streams, terminal `summary.json`, `checkpoint.json`, and
`payment-consent-failure-observation-v1.json`. Its447,480-byte PostgreSQL archive
was fully decoded with `pg_restore --file=/dev/null`. The first controller's
static-count assumption does not overwrite the runner's785 selected names.

Focused evidence is in `.cache/shuttle-work/linux-race-focus-v4/`; UI/API evidence
is in `ui-fixes-repository-proof-v1/`. The original failing UI cases and reviewed
drafts remain in `checkpoint44-ui-regressions-v1/` and
`checkpoint44-ui-fix-draft-v1/`. Both temporary stands are archived and removed,
with source and primary-preservation checks. Archive decoding is not a restored
runtime test. No Telegram Web message was sent for these gates.

Next: repeat all786 default Linux roots, check actual remote CI after publication,
and build/install/roll back the corrected assets on the owned primary. Joint
zitadeltg/browser CA and real Telegram WebView acceptance remain separate work.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
