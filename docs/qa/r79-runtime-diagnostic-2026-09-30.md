# R79 full runtime diagnostic

2026-09-30. **D-001 remains failed.** This diagnostic distinguishes late successful completion from a correctness failure; it is not a replacement acceptance gate.

## Scope and method

The native run used the three original modern runtime groups: meal choice, full choice across restart (ASCII, Cyrillic and escaped input), and the large catalog. Original scripts, 256 KiB choices, mutation/version/receipt/source checks, pagination and restart assertions were retained. Two external Go overlay files replaced only the completion observers. Product source, script budgets, delivery pacing and the original test files were unchanged. Parallelism remained four tests with GOMAXPROCS2.

The observer records a failed assertion for every original five-second miss, then allows natural completion within a fixed 15-second diagnostic bound. Early runtime return, query failure, incomplete observation, nonnil runtime result and an unjoined runtime fail separately. Cancellation joins each Run within a separate five-second cleanup bound. An earlier parent cancellation still wins.

The first diagnostic stopped before scenario execution: its query incorrectly treated an absent initial Telegram cursor as an observation error. Bot startup creates that cursor. The second external observer uses a scalar cursor subquery with missing value treated as zero (not ready); actual query errors still fail. The first failed run remains recorded and is not evidence of a product defect or successful scenario. No product or original-test edit was made for this repair.

## Result

Second diagnostic session10889 finished in 181.61 seconds, exit1:

- 91 natural completions and 91 successfully joined runtimes.
- 51 five-second failures; maximum observed completion 7.0724973 seconds.
- Zero other assertion failures, observer failures, failed joins or panics recorded.
- All three character variants reached their final full replacement and original equality checks. The unchanged scenarios produced 34 ASCII, 34 escaped and 20 Cyrillic observations, plus two meal and one catalog observation.
- All bound source and overlay hashes matched after the run; no observed source changes.

Thus the unchanged correctness assertions completed within the diagnostic bound for this run. The actual Go result remains **FAIL**, because the old five-second requirement was missed. This is not a race run, independent Functional QA, a production latency guarantee, or evidence that the wider composition is accepted. It establishes load-sensitive late completion in this environment, not a proven CI-only cause or a general performance improvement.

## Evidence and next boundary

Local artifacts: `qa.local/go-resume-20260929/r79-modern-runtime/full-scenario/`, including original/candidate copies, diffs, both overlays and hash manifests, `run-1.jsonl`, `run-2.jsonl`, exit files, `run-2-summary.json` and the source comparison. Preserve these alongside the historical failed original group. The canonical five-second tests and all product limits remain unchanged. Any later acceptance-contract change must be explicit and cannot be described as meeting the old bound.

Separately, the installed R79 revision2 conversation package passed all21 tests with real PostgreSQL and no skips (`r79v2-unit-1`); pinned affected lint had already passed. Those gates establish the narrow query optimization scope, not resolution of D-001.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
