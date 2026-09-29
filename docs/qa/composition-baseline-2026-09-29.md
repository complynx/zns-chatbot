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
