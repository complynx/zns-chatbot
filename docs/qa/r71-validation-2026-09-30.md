# R71 validation — 30 September 2026

## Scoped functional acceptance

Fresh source-blind [Functional QA02](functional-qa-knowledge-2026-09-30-02.md) completed scoped PASS on the frozen R71 image. It verified actual EN/RU UI, author-private drafts and explicit consent, authorized review, negative/unavailable/recovered classification, isolation, sanitized history, app restart/exact pending-inbox replay, current permission revocation and equivalent concurrent callbacks. No blocking defect found. The report's unexercised source-retirement, real-provider and other boundaries remain open. Root restored all six original Bob grants and left no pause/fault. Subsequent R79 source optimization is not deployed in this image and is not covered by this acceptance.

## Final affected checks

Revision2 is bound by its eight-file after-hash manifest. Unit2 exited0 with46 passed test events; PG2 session60504 exited0 with10 passed events; neither had failures or skips. Both used the same scoped commands below. The previously failing source test now reaches the actual authority boundary using a memo created by the trusted derived-memory API; source deletion and zero provider HTTP are asserted.

Fresh independent [Code QA136](code-qa-2026-09-30-136.md) found no P0–P2 defect and identified contradictory operator documentation. Root corrected that prose. Lint3 found only one golines line-wrap issue; root ran the pinned formatter on that single file, inspected the one-condition wrapping diff, and recorded final hashes in revision2/root-final-hashes.json. Lint4 session39772 exited0 with0issues. QA136 separately verified these two nonsubstantive deltas, closed its documentation finding and verified the other six files unchanged. No new runtime behavior was introduced after the passing tests.

R71 entered fresh independent Functional QA on a separate new synthetic stand. App/fake image digest is1d41a515bca34e88a4c447050d203aef0bfc09629fe899865f334c8f26edac11; both health checks and actual image IDs were verified. Prior stands and data were preserved. Public knowledge documentation now describes the existing private-draft/manual-author-consent requirement. This is not full memory/shared-knowledge acceptance or a green full-platform baseline.

## First revision evidence

Scope: eight frozen files bound by `qa.local/go-resume-20260929/r71-fixture-classifier/implementation/after-hashes.txt`. Root verified all hashes before unit execution and after the first PG/lint cycle. All remained unchanged during that cycle.

- Unit: `go test -json -count=1 -timeout=3m ./internal/sandbox -run '^(TestFixtureAssessment|TestModelFixture)'`, exit0; 31 passed test events, no failures/skips.
- PostgreSQL: `go test -json -count=1 -timeout=8m -parallel 4 ./integration -run '^(TestKnowledgeFixtureAssessment|TestKnowledgeCoordinator)'`, exit1; 9 passed events, one failed event. `TestKnowledgeFixtureAssessmentRechecksSourceBeforeWire` fails at line206 because the direct memo result has no read authorities. This stops before the intended source invalidation boundary; it does not prove that boundary works or fails.
- Pinned affected lint: first invocation stopped before analysis due to sandbox working-directory path resolution, exit3. Authorized second invocation completed exit1 with15 issues: goconst4, mnd3, paralleltest4, tparallel4. No suppressions or gate relaxation applied.
- Fresh independent [Code QA134](code-qa-2026-09-30-134.md) found a P2 strict JSON defect: case aliases map to the same Go struct field and can overwrite the programmed verdict. Current revision is not accepted.

Evidence: `r71-unit-1.jsonl`, `r71-pg-1.jsonl`, `r71-lint-1.log`, `r71-lint-2.log` and matching exit files under `qa.local/go-resume-20260929`. These local logs are not publication artifacts.

The original developer owns corrections in the same eight files. Source was released only after all root jobs were terminal. Revised hashes, affected gates and fresh Code QA are required. No image rebuild or new Functional QA acceptance occurred.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
