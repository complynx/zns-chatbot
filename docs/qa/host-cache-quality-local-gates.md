# Host/cache quality local gates

Candidate: c-host-cache-quality-fix-20260930. Independent Code QA224 static PASS
was supplied by the integrator. Branch codex/host-cache-quality starts at
ce9942711a24f852bd4ab6ae8971d345d297f019.

All nine worktree source files matched the frozen before hashes. All ten recorded
dependencies matched. The nine final files were copied verbatim from the frozen
candidate. Pinned formatting preserved every final SHA-256 hash. Original frozen
artifacts remain unchanged; no substantive fix or additional suppression was
needed.

Local checks on 2026-09-30:

- Pinned golangci-lint v2.14.0 fmt using platform/.golangci.yml: PASS.
- Affected lint on ./cmd/zns/... ./internal/agenthost/...
  ./internal/observability/... ./internal/bot/...: PASS, zero issues.
- Focused native tests using -count=1 and pattern
  Test(RuntimeAuth|ZitadelSecrets|KnowledgeCatalog|KnowledgePreparation|IdentityCache):
  PASS, 12 top-level tests, 33 test/subtest passes, zero failures.
- Bot validation using -count=1 and pattern ^TestKnowledgeTools: PASS,
  two top-level tests, ten test/subtest passes, zero failures.
- git diff --check: PASS.

The first lint invocation failed before analysis because the sandbox denied
working-directory resolution. The same command passed with approved filesystem
access. Existing exclusion warnings reported zero skipped issues. Formatter
emitted a working-directory pretty-print warning and exited zero.

Go build caches and lint cache were isolated under this worktree's
platform/qa.local/host-cache-quality-gates/. Raw evidence is in focused-tests.jsonl,
bot-validation-tests.jsonl, affected-lint.log and affected-lint-retry.log there.
The first test command compiled the bot package but selected no bot tests; the
separate bot validation command ran the two tests stated above. No shared DB or
Docker resources were used. This is local source/check evidence; it does not
claim a fresh black-box Functional QA gate.

Candidate patch SHA-256:
03a3c8fc8729a5cabd75328af85d6a8570040c72dc969b1a7e1d3a2747da63b7.
Candidate manifest SHA-256:
8211c0de31f9670a54c053e63992f74180eb7047231f448d6c08e66301750188.
Frozen inventory SHA-256:
686e4df3c885ca2d05d948b889bf588ac7d41471c2b540c07a53494db53999de.

Written by menu_retirement_fix (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
