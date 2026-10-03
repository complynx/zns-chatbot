# Code QA 408

Verdict: PASS for the static E206 read-only transport review scope. No actionable correctness or security findings identified.

Candidate: `qa.local/e206-transport-final-20261003/batch1/run-baseline.py`, SHA256 `3b5a7650ddf21ea8547289623136cf8404e87b85975576b5e628626bb4c4e291`.

Review manifest SHA256: `e5a8755e07da4558979d72627a433adf19954178a05ff151e1ca8ee799be064f`. All 21 bound entries matched their hashes before and after review, including the query SHA256 `4a58efe57b7b11f961a266426eb05c07c09034a141535f3734637a3b84f3b732`.

Reviewed the current requirements, controller source, both comparison diffs, producer, create recipe and profile template, input-manifest bindings, native CLI and archive dependencies, unchanged query source, and the 22-case and 31-case proof sources plus their pinned runner. No author reports, author gate outputs, previous QA findings, management material or outcome history were read.

The source preserves the read-only query guards, fixed producer profile and aggregate resource reservation. Admission validates original PG identity and healthy start, cluster/client custody, all native output-volume consumers, and the complete Created profile before the single START. The owned ID is captured before command receipt persistence. Native validation precedes fallible receipt writes; permissive receipt handling is limited to terminal observation, retention and owned cleanup, while any receipt failure keeps the final host result failed.

Retention verifies frozen source closure before accepting bounded size authority, rejects invalid ownership/member framing or aggregate size before raw copy, checks exact copied sizes and bounded hashes against the original delivery cutoff, and checks stored/native query exit and query identity. Failure paths preserve the first error, record incomplete retention, avoid retrying START/removal, and keep removal restricted to the exact validated owned helper. Successful host status requires complete retention and owned removal without earlier failure or cleanup errors.

The proof inventory retains 22 original cases and adds 31 distinct coupled boundary cases. The latter exercises actual call/validation functions and main terminal/finally branches with injected native and receipt failures; the original suite also covers delivery byte/time bounds and Created/stopped consumers. The runner requires assertions, pinned Linux Python and exact source hashes and case counts. This static review does not assert that those tests executed or passed.

Limitations: no test, runtime, SQL, Docker or shared lifecycle operation was executed. Passive profile bindings and source guards do not establish present stand identity, actual 178-object data preservation, runtime timing or Functional acceptance. Runtime custody and the allowed operator window remain separate gates. Only this report was written; reviewed source remained unchanged.

Written by code_qa_408 (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
