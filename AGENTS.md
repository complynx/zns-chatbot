# Rules

- KISS. Type-safe. Preserve active behavior; partial port != parity.
- Go: pinned golangci-lint, Golden + Nebius linters, testify.
- JS: ESLint, Prettier, Playwright. Real PostgreSQL integration tests.
- UI i18n: en + ru minimum. Catalog keys. Both locales in Functional QA.
- Free text/media: agent interprets intent. Pending form = hint, never trap next message.
- Update `management.local/PROGRESS.html` after stage gates, plan changes, blockers. Built != accepted.
- Keep management.local/PROGRESS.html plain semantic HTML without CSS or JavaScript. Use sections "Готово", "Сейчас", "Что ещё предстоит", "Вопросы для решения" with short task descriptions; distinguish historical scoped acceptance from current proof. Keep detailed evidence in reports/history. PROGRESS.md is a link only.
- Add unresolved choices that require Daniel to "Вопросы для решения" in management.local/PROGRESS.html, with known options and dependencies. Pending is not approved. Wait only on dependent work; continue independent work. Do not turn ordinary engineering decisions or unverified findings into approval requirements.
- `noqa`, `nolint`, `eslint-disable`, `@ts-ignore`, exclusions: very rare. Fix code first. Exception: smallest scope, concrete reason, Code QA review. Never weaken gates for green results.

- Synthetic stand data and stands may be changed or deleted when this does not conflict with another test. Assign stand/data ownership before concurrent work; isolate conflicting scenarios. This supersedes the earlier no-deletion pause constraint for synthetic test resources. Production remains separately authorized.

- Working progress and Kanban live only in ignored `management.local/PROGRESS.html` and `management.local/KANBAN.html`. Never stage them or recreate tracked root copies. Keep operational metrics, assignment/review counters, worker acknowledgments and current estimates in management.local too; tracked documents may contain stable links. Keep stable contracts and final acceptance reports in Git.

# Two QA gates per stage

- Staffing instruction from Daniel (2026-09-30): keep 2–3 developers continuously assigned to unfinished functional or architectural capabilities, with defect fixes running in parallel. Follow the approved C–E then remaining-parity sequence; do not invent unrelated features. Declare disjoint file ownership, refill capability assignments as they finish, and keep independent review gates. Use separate Codex review runs if slots become limiting, or report the concrete sustained slot requirement to Daniel. All assignments remain Codex-only under the temporary override below.
- Git workflow authorized by Daniel (2026-09-30): current branch is the integration branch. Each developer owns a separate branch/worktree and runs local focused gates there. Review immutable commit IDs. Serialize merges into the integration branch; verify affected integration gates after merge. Commits and merges are authorized locally; push and production remain separately authorized. Existing frozen candidates are migration inputs, not the default development workflow. See docs/git-development.md.
- Developer handoff requirement (2026-10-01): developer prepares the branch for merge, updates it against the integration branch, resolves conflicts and passes affected automated tests, pinned lint and formatting before requesting Code QA. Submit a clean worktree and exact commit/base plus gate evidence. Root does not take over developer gate cleanup or conflict resolution. Substantive post-review resolution requires fresh affected review; keep reviewed commits immutable while review runs.
- **Code QA:** fresh independent Senior agent. Original requirements + stage scope + diff; source allowed. Read-only. Check correctness, security, transactions, complexity, tests, dependencies, suppressions.
- Temporary user override (2026-09-30): use Codex for ALL developer and QA assignments until Daniel explicitly restores Claude availability. Do not launch or probe Claude. Suspend the 1:1 developer rotation and every-third Opus QA route; keep the fresh QA counter continuous and record Codex routing under this override. Existing Opus-authored work still requires independent Codex review.
- Alternate new developer assignments 1:1 between Opus 5.5 medium and Codex. Transfer a task to the other developer when repeated attempts fail or changes grow without a sound solution; reassess the approach against requirements and evidence.
- Count fresh Code QA requests from the 2026-09-29 resumption, starting at 1. Code written by Opus must always receive fresh independent Codex review; this takes precedence over routing every third request (3, 6, 9, ...) through `claude-opus-5-5` when the SSH Claude tunnel and model are available. Keep the counter continuous when the authorship rule overrides the route. Record the request number, authorship, route and any availability limit in the developer tracking record; do not pass that record or findings to reviewers. All fresh, independent, read-only review rules still apply.
- **Functional Senior QA:** separate fresh agent. Requirements + acceptance scope + sandbox access only; no implementation details. Black-box manual/agent flows, permissions, history/UI freshness, retries, concurrency, persistence, failures.
- Functional QA may request stand upgrades/new stand types, or build its own stands/tests. Own assigned test paths; no product edits. Preserve independent acceptance.
- Required stand: Telegram-like functional UI. Messages, buttons/keyboards, callbacks, edits, uploads, manual/agent flows as applicable. Match Telegram behavior; visual fidelity irrelevant. HTTP-only checks insufficient.
- No conversation history, prior findings or fix hints to reviewers. Reports: evidence + limitations; skipped != passed.
- One sandbox writer; no rebuild during QA. Separate report ownership.
- Fix verified defects; rerun checks; fresh affected reviews after substantive changes. Repeat until clean. Preserve staged diff.
- Both QA + required checks green: advance automatically. No extra permission wait. Production/publish/commit/push still need authorization.

- Execution coordination (2026-10-01): read `docs/coordination-process.md`. All new checks run on Linux Docker/WSL; no new native Windows checks, including Windows executables targeting Linux. Root grants two Linux heavy-check slots across isolated stands. Already-running Windows checks finish and occupy a transition slot until terminal evidence; no quiet-output restarts. Functional fault windows are prepared before arming, original budgets remain unchanged, owned synthetic downloads are allowed, and progress/board/reports are checkpointed together. Prioritize the finite C–E acceptance chain.

Details: `docs/go-migration.md`, `docs/code-quality.md`, `docs/coordination-process.md`.
