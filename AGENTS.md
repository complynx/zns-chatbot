# Rules

- KISS. Type-safe. Preserve active behavior; partial port != parity.
- Go: pinned golangci-lint, Golden + Nebius linters, testify.
- JS: ESLint, Prettier, Playwright. Real PostgreSQL integration tests.
- UI i18n: en + ru minimum. Catalog keys. Both locales in Functional QA.
- Free text/media: agent interprets intent. Pending form = hint, never trap next message.
- Update `PROGRESS.md` after stage gates, plan changes, blockers. Built != accepted.
- Progress short. Current tasks only in "Сейчас". Done moves out. Details in reports/history.
- `noqa`, `nolint`, `eslint-disable`, `@ts-ignore`, exclusions: very rare. Fix code first. Exception: smallest scope, concrete reason, Code QA review. Never weaken gates for green results.

# Two QA gates per stage

- **Code QA:** fresh independent Senior agent. Original requirements + stage scope + diff; source allowed. Read-only. Check correctness, security, transactions, complexity, tests, dependencies, suppressions.
- **Functional Senior QA:** separate fresh agent. Requirements + acceptance scope + sandbox access only; no implementation details. Black-box manual/agent flows, permissions, history/UI freshness, retries, concurrency, persistence, failures.
- Functional QA may request stand upgrades/new stand types, or build its own stands/tests. Own assigned test paths; no product edits. Preserve independent acceptance.
- Required stand: Telegram-like functional UI. Messages, buttons/keyboards, callbacks, edits, uploads, manual/agent flows as applicable. Match Telegram behavior; visual fidelity irrelevant. HTTP-only checks insufficient.
- No conversation history, prior findings or fix hints to reviewers. Reports: evidence + limitations; skipped != passed.
- One sandbox writer; no rebuild during QA. Separate report ownership.
- Fix verified defects; rerun checks; fresh affected reviews after substantive changes. Repeat until clean. Preserve staged diff.
- Both QA + required checks green: advance automatically. No extra permission wait. Production/publish/commit/push still need authorization.

Details: `docs/go-migration.md`, `docs/code-quality.md`.
