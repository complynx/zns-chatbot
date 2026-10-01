# Parallel Git development

Integration branch: `feature/go-platform-sandbox`. Daniel authorized consolidating
the current work and switching development to Git on 30 September 2026.

Heavy-check slots, precise Functional QA permissions, prepared fault windows and
batched progress checkpoints follow [execution coordination](coordination-process.md).

The initial checkpoint preserves applied work, documentation and known failures.
It is not a release or a new acceptance verdict. Unapplied candidates remain in
their preserved local evidence directories until moved into task branches.

1. Assign each developer a `codex/` task branch and separate worktree under
   `.worktrees/`. State file ownership before editing. Start from the integration
   checkpoint; do not edit another worktree or the integration checkout.
2. Complete the feature and run pinned formatting, affected lint and focused
   tests locally. Use a separate PostgreSQL database/stand for concurrent tests;
   the shared database and integration stand require exclusive ownership.
   The developer also updates the branch against the integration branch and
   resolves conflicts in their own worktree. Repeat affected gates after resolving
   conflicts. A failing affected gate is not a ready Code QA handoff.
3. Commit explicit task paths, including focused tests. Record exact commands,
   outcomes and limitations. Never commit credentials, caches, local evidence
   dumps or generated binaries. Preserve original evidence rather than deleting it.
4. Fresh independent Code QA reviews the exact commit and requirements. Provide
   source/diff, not author reports or prior findings. A changed commit needs
   affected fresh review; reviews of earlier content do not certify new content.
5. Root grants one merge slot at a time. A developer may merge only the approved
   commit into the integration branch while holding that slot. Conflicts require
   explicit resolution and affected verification/review. Do not force-reset or
   overwrite another task's work. Root may perform the same serialized merge.
   The developer owns merge preparation and conflict resolution. If the integration
   base advances after review, stop and prepare the updated branch; substantive
   resolution receives fresh affected review before merging. Metadata-only rebases
   must retain exact reviewed source and record their equivalence and base binding.
6. Run affected integrated checks and update the ignored management.local/PROGRESS.html. Fresh Functional QA
   uses a frozen stand and EN/RU Telegram-like UI. A merged feature is implemented,
   not automatically accepted. Both QA gates remain required for stage acceptance.

Commits/branches/merges are locally authorized. Push, publication and production
cutover remain separate. Keep current failing baseline and untested flows visible.
Do not amend a reviewed commit while its review is running; make a successor.

## Queue and acceptance batches

Keep at most three finished branches waiting for review or merge. At the limit,
developers help clear their gate/conflict/readiness dependencies before opening
another finished handoff. Maintain the agreed two–three capability developer lanes;
this limit concerns the handoff queue, not all active development.

Functional QA consumes an immutable batch identified by integration commit and
image digests. New merges belong to the next batch; they do not rebuild an active
QA stand. Build each required image once per exact source/build configuration and
reuse its immutable digest across isolated stands. Changed build inputs require a
new receipt; matching commit names alone do not prove matching images.

Developer handoff contains: branch, exact commit/base, owned paths, clean status,
conflict resolution, formatting/lint/test commands and outcomes, evidence paths,
remaining limitations and required independent acceptance scope. No ready handoff
with failing affected gates. Gate evidence is for coordination; reviewers receive
requirements and immutable diff/source, not author conclusions or earlier findings.

FQA lead manages stand preparation, freeze/release, reviewer allocation and the
requirement-to-scenario matrix. Kanban manager owns observed queue/wait metrics
and task handoffs. Root owns integration/release decisions and verified shared gate
outcomes. Record waiting times only from observed events; unknown historical starts
stay unknown. Test counts are evidence quantities, never a readiness percentage.

Existing candidates enter this workflow by checking their before/dependency
hashes, applying their exact final files on a task branch, and committing them.
Record the candidate-to-commit mapping. If the base has changed, resolve and review
the resulting complete diff instead of silently treating old approval as current.
