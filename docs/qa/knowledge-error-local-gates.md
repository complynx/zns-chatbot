# Knowledge error precedence local gates

Branch codex/knowledge-error-precedence; initial base ce9942711a24f852bd4ab6ae8971d345d297f019. Ownership: knowledge_context.go, new knowledge_sanitize_failure_internal_test.go, and this report. No shared Core, schema, DB, Docker, network, or public API edits.

Transfer guards matched frozen rev3: original knowledge_context.go B9DE9B5E1D98A4CABF887755CE35C9F781A2E8DD47F11205C861CAC10E88CC72; imported final 536F9E1A69AC86557FBF56182CB1535A84C1D4697D7E399B6ECE7073B162A2A8; imported test E4026C246B9EC68B6788D0C63464E7C566B22D604F36D96E82B83C0E6CEC67D1. Shared Core database_error.go 1EFA57E8E7DFCB2FCA579D94F113055B77A5D31C48396D4E7ED278B6F571DE73; go.mod D7527B51CE5D9A263798095662A825604300ED9A3A9001EB3566710D4384EB97; go.sum A6B60302A3E8714F159467519D18783799214DC03E6E886F2846EDDC2FCAEDD2.

Reproduction on imported rev3: TestSanitizeKnowledgeReadsAggregatedConnectionFailures exits1. Four failed subtests: caller canceled=true, both context.Canceled/context.DeadlineExceeded, for mixed inside connection and cancellation sibling first. Expected database unavailable; actual caller cancellation. EOF-first siblings were positive controls. Injected DialFunc constructs real pgconn ConnectError without network I/O. Predecessor freeze and earlier QA evidence remain unchanged.

Fix inspects every joined connection subtree and every dial cause inside concrete ConnectError before accepting matching caller cancellation. Positive leaves return only Core.ErrDatabase. Cancellation-only connections preserve matching caller cancellation, while live-caller driver cancellation remains ErrDatabase. Public SQL/serialization markers and PgError precedence survive. The reader retains current event rights checks, deletion redaction, bounded projections, spent read slots, and stops subsequent reads on failure. All sentinel/privacy checks remain.

Initial corrected gates on base ce994271:
- Full agenthost package tests exit0.
- Focused TestSanitizeKnowledgeReads* JSON run: 63 passing test/subtest events, 0 failures.
- Pinned formatting rewrite and final --diff exit0; no final source diff. git diff --check exit0.
- Sandboxed full lint initially failed before analysis: EvalSymlinks working-directory access denied. Authorized local escalation resolves that environmental error.
- Full pinned agenthost lint then exits1 with four untouched baseline findings: knowledge_preparation_internal_test.go:18 embeddedstructfieldcheck; knowledge_preparation_internal_test.go:109 staticcheck QF1003; knowledge_registry_internal_test.go:31 embeddedstructfieldcheck; knowledge_registry.go:39 funlen 113>100. Owned-file issues were corrected; no suppression/config edits.
- Additional whole-changed-file lint --new-from-rev ce994271 --whole-files exits0, 0 issues. This focused result did not claim the full gate passed.

Caches isolated at this worktree .gate-cache/go and .gate-cache/lint, GOPROXY=off. Root requested rebase onto current feature/go-platform-sandbox where its independently integrated quality commit fixes the four baseline findings. Final post-rebase full gates follow below; initial failures above remain historical evidence, not suppressed.



## Final post-rebase gates

Rebased onto feature/go-platform-sandbox parent d6e667bbe3a8b4cd0837d6e476773d91f34b850a, which includes root quality integration75a65e21. Full pinned lint: exit0, 0 issues. Full agenthost package tests: exit0. Final owned-source formatter --diff: exit0, no diff. git diff --check: exit0. No focused-lint substitute is used for final acceptance. Logs are retained in this worktree .gate-cache/final-lint.log, final-tests.log, final-format.log; initial reproduction/failing-lint diagnostics are preserved above. No merge or push. Fresh independent Code QA and separate Functional QA remain pending.

Final source SHA256:

- platform/internal/agenthost/knowledge_context.go: 89B95EDA9D41F1432C3E8604D4C2ED577066DD252FD6D8D82589867A3E1ACCF9
- platform/internal/agenthost/knowledge_sanitize_failure_internal_test.go: 3F954F0E1F51B677CB687B9BBD8DDC189CC38594C704FCD7F317E904E3F3B1BD
- platform/internal/core/database_error.go: 1EFA57E8E7DFCB2FCA579D94F113055B77A5D31C48396D4E7ED278B6F571DE73
- platform/go.mod: D7527B51CE5D9A263798095662A825604300ED9A3A9001EB3566710D4384EB97
- platform/go.sum: A6B60302A3E8714F159467519D18783799214DC03E6E886F2846EDDC2FCAEDD2

Written by provider_compatibility_developer (GPT-6/Codex)
on behalf of Daniel Drizhuk

## Merge preparation against integration133104f6

Rebase onto exact integration parent133104f6c26286921cc702074ac2e3036019b53a completed without conflicts. Reviewed commit a88c23c93b12237a12acaa67499ef3ca23713250 remains preserved. Both owned source files have identical Git blobs before and after rebase; no source correction or substantive resolution occurred. Only this gate addendum changes beyond the reviewed source.

Full affected tests: go -C platform test ./internal/agenthost -count=1, exit0. Full pinned lint: golangci-lint.exe run --config .golangci.yml --allow-serial-runners ./internal/agenthost, exit0, 0 issues. Initial merge-prep lint attempt exit3 due global parallel-run lock; serial-run mode waits for that lock without changing analysis. Owned-source pinned fmt --diff exit0, empty diff; committed diff --check exit0.

All isolated caches and original evidence were preserved by moving .gate-cache into ignored platform/tools.local/knowledge-error-gates within this same owned worktree. Prior .gate-cache paths in this report are historical; evidence remains in that new location. New merge-prep gate logs are retained there. Clean worktree required at handoff. No other worktree, shared database, Docker resource, integration merge or push was touched.

- platform/internal/agenthost/knowledge_context.go: reviewed/current Git blob dba420d8697803997862d1d32687ff7baf6119d0; SHA256 89B95EDA9D41F1432C3E8604D4C2ED577066DD252FD6D8D82589867A3E1ACCF9.
- platform/internal/agenthost/knowledge_sanitize_failure_internal_test.go: reviewed/current Git blob 9671ff5953956883a21a808e7b7f7315be76d995; SHA256 3F954F0E1F51B677CB687B9BBD8DDC189CC38594C704FCD7F317E904E3F3B1BD.

Written by provider_compatibility_developer (GPT-6/Codex)
on behalf of Daniel Drizhuk
