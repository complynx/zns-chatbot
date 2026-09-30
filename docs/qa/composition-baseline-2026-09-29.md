# Composition baseline — 2026-09-29

**Developer gate evidence; not acceptance. Do not give this report or its triage to independent reviewers.** Source: `b99c4822ba9110dde5009e0aac738797cbb5ae66`. Subsequent fixes require new evidence. Preserved source inputs are recorded in [provenance](../go-source-provenance.md).

| Gate | Result | Limit |
| --- | --- | --- |
| Platform compile, five authored roots | PASS, exit 0 | Compile-only; no tests |
| Importer compile, two packages | PASS, exit 0 | Compile-only; no tests |
| Pinned formatter 2 | PASS, exit 0 | Formatting only |
| Focused PostgreSQL, five top-level scenarios | PASS, 8.187s, no skips | Developer scope: delivery split-role/restart and revocation, native retention FIFO, external memo deletion next plan, memo-delete retained list |
| Full native/PostgreSQL | FAIL, exit 1; integration 477.001s | 2237 pass events, 104 failed test events, 26 skipped events; counts include subtests and are not independent scenario counts |
| Strict pinned lint-all-1 | FAIL, exit 1 | 47 issues: gocognit 3, goconst 40, govet 1, mnd 1, nestif 1, whitespace 1; no exemptions |
| SQLC regeneration diff | FAIL, exit 1 | Botdelivery generated `NotBefore` differs: `time.Time` versus `pgtype.Timestamptz`; config override correction to preserve legacy typed time pending |
| Product Functional QA | NOT RUN | Independent plan prepared; Telegram-like stand pending |

The [sanitized event summary](composition-baseline-2026-09-29.json) retains only the commit, counts and package/test names from `qa.local/go-resume-20260929/native-pg-all-1-summary.json`; no raw logs, response bodies, credentials or environment values are copied. Skipped tests did not pass. Initial compile failure evidence remains local; the compile passes above are the subsequent successful runs.

## Developer triage — not independent findings or acceptance

Current investigation identifies profile transient private values lost through redaction, a registration lock-prelude union bug, and identity/async/broadcast fixture gaps. These are developer findings or hypotheses for targeted reproduction and fixes, not proof that every failed event has a known cause. Neither checks nor fixtures may be weakened to obtain a passing result. Required product fixes need affected regression evidence and fresh independent review.

Code QA 1 has a scoped static PASS; Code QA 2 requires changes for a restricted Bot DB read. Their separate reports preserve scope and limits. Code QA 3 completed against its frozen source with CHANGES REQUESTED; response metadata verifies actual `claude-opus-5-5`. Its [portable report](code-qa-2026-09-29-03.md) preserves findings and static-review limitations. Source work has resumed for affected fixes; baseline counts remain unchanged. None of these results accepts the full composition.

## Completeness addendum — 2026-09-29

The original native/PostgreSQL run was **INCOMPLETE**, not a completed full suite. Its recorded 2237 pass events, 104 failed test events and 26 skipped test events remain accurate, but panic `TestOrderFixturesEnableDeadlineAndRURouting` (raw log line 24012) left **877 started tests without a terminal event**. Exit 1 remains a failure; these counts cannot measure all remaining defects or full-suite coverage. Original evidence and counts above are retained without rewriting history.

The second run was also **INCOMPLETE**: panic in `TestCreditsUnlimitedPolicySourceIsAccount`, `credits_cutover_policy_test.go:53`, indexed empty asynchronous messages at -1. It recorded **1844 passed test events, 12 failed, 23 skipped and 920 started without terminal events**. A further 32 package skip events denote packages with no tests and are not test skips. Twelve is not the number of all remaining suite failures.

Completeness evidence is local: `qa.local/go-resume-20260929/native-pg-all-1-completeness.json` and `native-pg-all-2-summary.json`. A new complete run is required after fixture panic repairs; neither skips nor weakened assertions substitute for completion. This addendum corrects interpretation of developer evidence, not a QA verdict.
## Complete run 3 — terminal evidence

Native/PostgreSQL run 3, session 66612, completed with **exit 1**: **3250 passed, 294 failed and 28 skipped test events**, including subtests. Panics: **0**. Started tests without terminal events: **0**. The only failed package was `github.com/complynx/zns-chatbot/platform/integration`. All **1980 source files** and inventory were unchanged (ChangedFiles 0, InventoryChanges 0). This is a complete failing run, unlike runs 1 and 2; the event counts are not independent defect counts.

Evidence: `qa.local/go-resume-20260929/native-pg-all-3-summary.json`, `native-pg-all-3-failures-skips.json` and `native-pg-all-3-source.json`. Full pinned lint 5 also terminated, exit 1 with 23 issues. The global freeze was lifted for an assigned repair batch after this terminal evidence. No product-stage acceptance is implied. A new full run and affected independent reviews are required after fixes.

## Complete run 4 — 2026-09-30 terminal evidence

Native/PostgreSQL run 4, session 70655, completed with **exit 1**: **3355 passed, 222 failed and 28 skipped test events**, including subtests. Panics: **0**. Started tests without terminal events: **0**. Only `github.com/complynx/zns-chatbot/platform/integration` failed; its elapsed time was **1408.613s**. All **1984 source files** and the inventory were unchanged (ChangedFiles 0, InventoryChanges 0). The source freeze ended after completion.

Evidence: `qa.local/go-resume-20260929/native-pg-all-4-summary.json`, `native-pg-all-4-failures-skips.json`, `native-pg-all-4-source.json` and the full `native-pg-all-4.jsonl` log. Full pinned lint 9 passed with zero issues. Independent [Code QA 24](code-qa-2026-09-30-24.md) passed its static scope; it is not full-suite or Functional QA acceptance.

Run 4 replaces run 3 as the latest complete diagnostic baseline. The change from 294 to 222 failed events is not a count of repaired defects: test composition and subtests changed. Integration fixes, a green full baseline and independent Functional QA remain required. No readiness percentage is inferred from the pass-event ratio.
