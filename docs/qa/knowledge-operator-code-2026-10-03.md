# Code QA 393

## Findings

No actionable correctness, security, transaction, concurrency, dependency or suppression findings in the reviewed delta. **PASS for this source scope only.** This is not Functional QA or whole-stage C acceptance.

## Immutable review identity

- Candidate: `91d51754940d3062f81d5e4480009f2d4a4dacff`.
- Base: `1683690ccecc6ae8fdf9e7f25b7c4559e0f53778`.
- Source worktree: `.worktrees/c-product-boundary-20261003`.
- Before and after source inspection, `git status --porcelain=v1` was empty, HEAD was the candidate, and `git merge-base <candidate> <base>` was the base.
- Full exact diff contained only the six assigned paths. Source was inspected with `git show` and `git grep` at immutable IDs.
- Reviewer: fresh independent Codex agent, GPT-6 family, Codex harness. Exact runtime model slug is not exposed to this reviewer.

## Scope and evidence

Reviewed the base contracts in `AGENTS.md`, `docs/go-migration.md`, `docs/architecture-refactor-plan.md`, `docs/architecture-ownership.md`, and `docs/code-quality.md`. Reviewed the complete exact six-path diff and relevant fixture guards, knowledge locking/authority/readback, schema, product fixture and role-allocation source.

- Finite binding: `platform/internal/sandbox/knowledge_fixture.go:27` restricts stand, action, scope and permission. Actor is fixed at `:15`. Only general, sandbox-festival and sandbox-past plus review/curate are accepted. There is no content or proposal-consent control.
- CLI admission: `platform/cmd/zns/knowledge_fixture.go:20` rejects incomplete/invalid controls; `platform/cmd/zns/fixture.go:20` parses both configurations before dispatch and rejects combined enabled controls at `:30`. Invalid controls fail before database access.
- Existing registration safety boundary: `platform/internal/sandbox/knowledge_fixture.go:63` retains the shared fixture advisory transaction lock; `:66` calls the existing operator guard. `platform/internal/sandbox/registration_fixture_roles.go:30` checks exact database/login, owner, nonprivileged role and absence of memberships; `:73` locks registration events then stable users and `:83` verifies all synthetic identities and markers.
- Knowledge-specific guard: `platform/internal/sandbox/knowledge_fixture.go:129` checks the two table owners, `:138` verifies all three fixed scope bindings, and `:147` checks required metadata privileges while rejecting content-table privileges and broader scope/ACL writes. The fixture does not grant database privileges itself.
- Atomicity and concurrency: `platform/internal/sandbox/knowledge_fixture.go:76` locks Bob before taking the permanent general gate at `:81` and optional destination at `:86`. This preserves actor-before-knowledge-scope order and general-before-destination order used by `platform/internal/knowledge/command_boundary.go:32` and `platform/internal/knowledge/authority/authority.go:124`. Guard and mutation share one transaction; every precommit error returns through rollback.
- One-row idempotence: `platform/internal/sandbox/knowledge_fixture.go:95` inserts one fixed actor/scope/permission leaf with conflict suppression; `:102` deletes that exact leaf. The schema primary key in `platform/internal/store/migrations/026_knowledge.sql:13` enforces uniqueness. Repeating grants/revokes does not alter other capabilities or content.
- Current domain readback: `platform/internal/sandbox/knowledge_fixture.go:110` commits before calling the real `knowledge.Service.Scope` at `:113`. `platform/internal/knowledge/scope_pages.go:45` rechecks known actor and reads only event, phase and explicit current review/curate grants. `platform/internal/knowledge/types.go:40` exposes exactly those four fields. Readback is a subsequent current observation, not an atomic assertion of the mutation's committed snapshot; a concurrent later control can legitimately change it. A readback error can follow a successful commit, and bounded idempotent retry is safe.
- F04/F06/F07 preservation: the delta changes only explicit current-role ACL leaves. Domain authorization and proposal/version/body/destination-bound consent remain untouched; no generic consent is introduced. Existing permission locking and derived-proposal authority are retained in `platform/internal/knowledge/authority/authority.go:62` and `:145`.
- Focused tests inspect the finite validation matrix, readback shape and invalid/combined dispatch. The real-PG integration test covers private role composition, rejection of owning identity, repeated revoke/grant, unrelated permission/event preservation, content-privilege rejection and synthetic identity tampering. No dependencies, linter suppressions, domain behavior, locale, ingress or replay features are changed.

## Verification and limitations

- `git diff --check <base> <candidate>` completed with exit 0.
- Git/source inspection only. No native Windows test, lint or build was started. No Docker, SQL, stand or product mutation was performed.
- No runtime gates were executed or developer gate evidence consumed. Tests were reviewed as source, not reported as passing execution. The integration test explicitly skips without a dedicated owner URL; a skip cannot prove composition.
- Real-PG timing/concurrency, all six scope-permission mutation combinations, rollback/failure injection, actual operator grants and CLI execution remain runtime verification concerns. This source review does not establish their execution results.
- No management files, other QA reports, developer handoffs, prior findings or implementation history were consulted. No Functional acceptance is inferred.

Written by Code QA 393 (GPT-6 family/Codex)
on behalf of Daniel Drizhuk
