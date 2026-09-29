# Rules

- KISS. Type-safe. Preserve active behavior; partial port != parity.
- Go: pinned golangci-lint, Golden + Nebius linters, testify.
- JS: ESLint, Prettier, Playwright. Real PostgreSQL integration tests.
- UI i18n: en + ru minimum. Catalog keys. Both locales in Functional QA.
- Free text/media: agent interprets intent. Pending form = hint, never trap next message.
- Update `PROGRESS.html` after stage gates, plan changes, blockers. Built != accepted.
- Keep PROGRESS.html plain semantic HTML without CSS or JavaScript. Use sections "Готово", "Сейчас", "Что ещё предстоит", "Вопросы для решения" with short task descriptions; distinguish historical scoped acceptance from current proof. Keep detailed evidence in reports/history. PROGRESS.md is a link only.
- Add unresolved choices that require Daniel to "Вопросы для решения" in PROGRESS.html, with known options and dependencies. Pending is not approved. Wait only on dependent work; continue independent work. Do not turn ordinary engineering decisions or unverified findings into approval requirements.
- `noqa`, `nolint`, `eslint-disable`, `@ts-ignore`, exclusions: very rare. Fix code first. Exception: smallest scope, concrete reason, Code QA review. Never weaken gates for green results.

# Two QA gates per stage

- **Code QA:** fresh independent Senior agent. Original requirements + stage scope + diff; source allowed. Read-only. Check correctness, security, transactions, complexity, tests, dependencies, suppressions.
- Count fresh Code QA requests from the 2026-09-29 resumption, starting at 1. Route every third request (3, 6, 9, ...) through `claude-opus-5-5` when the SSH Claude tunnel and model are available. Record the request number, route and any availability limit in the developer tracking record; do not pass that record or findings to reviewers. All fresh, independent, read-only review rules still apply.
- **Functional Senior QA:** separate fresh agent. Requirements + acceptance scope + sandbox access only; no implementation details. Black-box manual/agent flows, permissions, history/UI freshness, retries, concurrency, persistence, failures.
- Functional QA may request stand upgrades/new stand types, or build its own stands/tests. Own assigned test paths; no product edits. Preserve independent acceptance.
- Required stand: Telegram-like functional UI. Messages, buttons/keyboards, callbacks, edits, uploads, manual/agent flows as applicable. Match Telegram behavior; visual fidelity irrelevant. HTTP-only checks insufficient.
- No conversation history, prior findings or fix hints to reviewers. Reports: evidence + limitations; skipped != passed.
- One sandbox writer; no rebuild during QA. Separate report ownership.
- Fix verified defects; rerun checks; fresh affected reviews after substantive changes. Repeat until clean. Preserve staged diff.
- Both QA + required checks green: advance automatically. No extra permission wait. Production/publish/commit/push still need authorization.

Details: `docs/go-migration.md`, `docs/code-quality.md`.
