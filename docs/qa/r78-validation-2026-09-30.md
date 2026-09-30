# R78 notification follow-up validation

The notification tests now separate fatal database failures from ordinary provider failures. A successful Telegram send remains recorded before follow-up. The fatal branch observes the real receipt trigger exception and asserts no later local SQL or API/Host/Telegram request before that invocation returns. The provider branch observes a real non-SQL identity-provider error and normal deferred retry accounting.

Fresh independent [Code QA138](code-qa-2026-09-30-138.md) reviewed the full external candidate and found no actionable issues. Root verified both current and candidate hashes before applying revision3. The focused real-PostgreSQL run `r78-pg-2.jsonl` passed all10 test events with no failures/skips, exit0. Both groups cover orders, registration, massage and food.

The checks preserve the first sent message ID, pending continuation, same-chat successor ordering, distinct successor receipt, exact sink IDs and no new requests on terminal replay. Independent-lane delivery before the fatal SQL boundary is allowed; later work in the failed invocation is forbidden.

Root pinned-formatting changed only line wrapping. The first lint then reported appendAssign; the equivalent two-statement append assignment resolved it without changing expected values or aliasing. QA138 verified these nonsubstantive differences against the reviewed candidate and final hash. Final affected lint `r78-lint-2.log` exited0 with0issues.

Limitations: the tests advance retained lease clocks after a stopped invocation and reconstruct the Bot owner. They do not prove natural two-minute lease expiry, supervisor/process replacement, real provider availability, full-platform baseline, or independent Functional QA. Earlier failed attempts remain in local evidence; no product notification behavior changed in R78.

Evidence: `qa.local/go-resume-20260929/r78-notification-followup/` contains originals, candidate revisions, full diffs, guarded application hashes and final formatter/lint bindings. Root runtime logs and exit files are adjacent under `go-resume-20260929`.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
