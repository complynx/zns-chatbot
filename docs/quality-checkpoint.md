# Quality checkpoint, 2026-09-25

Accepted scope: stage-1 sandbox and initial orders domain, before orders API/UI.
This is not acceptance of the complete Python migration.

- Code QA pass 1 found two defects: historical extra price reconciliation used
  the new catalog price, and booking confirmation checked time before waiting
  for the slot lock. Both were reproduced and fixed with PostgreSQL tests.
- Fresh Code QA pass 2 found no substantive defects. Static review only.
- Full local gate passed: Golden/Nebius GolangCI-Lint, ESLint, Prettier, module
  checks, vet/build, Linux PostgreSQL race tests, identity fuzz, live smoke and
  Edge mouse/emulated-touch acceptance.
- Fresh independent Functional QA passed manual/agent continuity, permissions,
  isolation, one-seat contention, stale callbacks, 429/deleted-message recovery,
  and message/history persistence across bot/fake restart. Its own browser
  harness and report are in `qa.local/functional-quality-pass1/`.
- No physical touch-device test or real Telegram/OpenAI call was performed.
  Orders UI and production identity remain subsequent acceptance scopes.
- Staged diff remained empty. No commit, push or production change.

Next checkpoint: orders HTTP contract and Telegram ordering/payment interactions.
