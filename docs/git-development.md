# Parallel Git development

Integration branch: `feature/go-platform-sandbox`. Daniel authorized consolidating
the current work and switching development to Git on 30 September 2026.

The initial checkpoint preserves applied work, documentation and known failures.
It is not a release or a new acceptance verdict. Unapplied candidates remain in
their preserved local evidence directories until moved into task branches.

1. Assign each developer a `codex/` task branch and separate worktree under
   `.worktrees/`. State file ownership before editing. Start from the integration
   checkpoint; do not edit another worktree or the integration checkout.
2. Complete the feature and run pinned formatting, affected lint and focused
   tests locally. Use a separate PostgreSQL database/stand for concurrent tests;
   the shared database and integration stand require exclusive ownership.
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
6. Run affected integrated checks and update PROGRESS.html. Fresh Functional QA
   uses a frozen stand and EN/RU Telegram-like UI. A merged feature is implemented,
   not automatically accepted. Both QA gates remain required for stage acceptance.

Commits/branches/merges are locally authorized. Push, publication and production
cutover remain separate. Keep current failing baseline and untested flows visible.
Do not amend a reviewed commit while its review is running; make a successor.

Existing candidates enter this workflow by checking their before/dependency
hashes, applying their exact final files on a task branch, and committing them.
Record the candidate-to-commit mapping. If the base has changed, resolve and review
the resulting complete diff instead of silently treating old approval as current.
