# R98 transitive receipt SQL boundaries

Scope: context-aware SQL failures in the reachable retired-receipt helpers, with explicit JSON decoding for batch plans and assignment transitions. No new receipt or resume authority is introduced.

The combined candidate comprises 12 product files and eight new or adjusted test files. It preserves expected missing-row outcomes, role fallbacks, current authorization, exact canonical witnesses and source equality. JSON decode errors remain ordinary. Resetting the locked batch plan before decoding preserves pgx's existing null and omitted-field behavior.

## Evidence

- `r98-red-1`: 77 FAIL and 69 PASS events on original product sources with the new checks.
- `r98-green-1`: 146 PASS, no FAIL/SKIP, external candidate.
- `r98-packages-1`: both complete affected packages, 277 PASS and no FAIL/SKIP, external candidate.
- `r98-receipt-pg-1`: exit 0 for the existing retired receipt, target mapping and memo permission cases, plus the R97 Begin/Commit fault checks, using the pinned candidate.
- Independent Code QA168: no actionable findings; all 20 final and 17 original file hashes verified.

Root applied all 20 files after checking original and final hashes. `r98-applied-1` passed the selected R98 unit/PG cases and existing receipt cases. Its exit was 1 because a separate external R97 runtime test used an incorrect hard-coded inbox update ID. The R98 assertions did not fail. That runtime proof remains unaccepted and its failed evidence is preserved.

Pinned lint `r98-lint-1` found five test-only issues: two modernization suggestions, one assertion helper preference, and two rules concerning the same sequential PostgreSQL subtests. The repair uses isolated per-case operation keys before enabling parallelism. QA169 found no defect in these two R98 test files; its separate runtime-test finding remains open. The first repair snapshot failed compilation because its nested composite literal omitted a required type. The preserved v2 uses an ordinary initializer and explicit embedded error assignment. `r98-packages-final-2` passed both complete packages: 277 PASS, no FAIL/SKIP. Root hash-guarded these exact candidates into the tree. `r98-lint-2` exited 0 with zero issues. All 20 final R98 source hashes are recorded in `r98-final-source-hashes.json`. No exclusions or suppressions were added.

## Limits

The source and focused tests establish the listed SQL/JSON boundaries; they do not prove every statement occurrence through a real driver, full bot behavior, independent Telegram-like Functional acceptance, or production readiness. The fake-row cases complement actual PostgreSQL JSON and transaction checks. Shared helpers retain their existing broader callers; full composition, race and stage acceptance remain open.

Artifacts, full snapshots, overlays and preserved failures: `qa.local/go-resume-20260929/r98-*`.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
