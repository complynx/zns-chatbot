# Shared avatar draft source acceptance — 2026-10-19

This record covers source schema55/mvp62. It does not claim the completed visual
editor or production readiness. No candidate image was built or installed, no
production deployment or publication was performed, and no real Telegram messages
were sent. The installed primary remains schema49/mvp56.

## Verified behavior

The final isolated PostgreSQL command passed all 18 selected avatar integration
roots: ten new draft scenarios and eight existing generation/transform scenarios.
They exercise native upload beyond the JSON limit, native/agent API handoffs,
historical replay after later edits and application restart, imports after
Telegram file loss, owner isolation, stale versions, parallel quota/CAS writes,
byte limits, failed-validation atomicity and session expiry during processing.
They also check draft output schemas, native action chronology, preservation of HTTP 413 for a known oversized
Telegram source before I/O, binary upload bounds and that existing JSON limits remain. The native/API draft path performs
no Loom run creation; its main handoff scenario keeps the mock Loom unavailable.

Two migration scenarios verify empty Down/Up, refusal after originals exist and
refusal during an uncommitted original write. The latter expects PostgreSQL
lock-not-available before any destructive statement; the writer then commits
and the original remains readable.

These are HTTP integration tests with mock Telegram/IAM/Loom peers and a real
isolated PostgreSQL. They are not a real-agent conversation or browser UI QA.
The existing source-loss/document-delivery scenarios remain mock delivery tests.
Full editor acceptance on an owned runtime stand, including agent conversations
and task-free native send with Loom stopped, remains outstanding.

Final Linux checks passed 59 unit roots across avatar, renderer, database, API
and task packages. The 25 database-dependent roots in that no-DSN unit command
skipped and are not counted as PostgreSQL proof. Project lint passed; the final
critical-path test has a separate follow-up lint record. The SQLC model update
is generated from schema55.

## Reviews and retained failures

Independent initial correctness and adversarial quality reviews found no
substantive defect. A fresh review found a rollback race: an emptiness check
could miss a concurrent first original before DROP. Exclusive NOWAIT table
locks now precede the check, and the concurrent-writer scenario passes.
Final fresh review and documentation checks are recorded in the source gate.
A further source inspection found that the native adapter mapped the file
reader's HTTP413 to an authentication HTTP503. Typed source errors now keep
their status, and the oversized-import scenario passes without Telegram I/O
or partial draft/receipt creation.

The initial PostgreSQL run had four test-fixture failures: a helper supplied
write-only idempotency keys to read methods, and another expected exactly four
module methods after discovery grew to ten. The fixtures were corrected without
relaxing API validation or removing declared-method checks. Later 15-, 16-, 17- and
18-root commands passed. Initial lint findings were fixed. All attempts remain
separate; an earlier failed command is not represented as a pass.

Ignored evidence is under `.cache/shuttle-work/avatar-drafts-v1-qa/`:
`pg-v5/checkpoint.json`, `linux-unit-v3-checkpoint.json`, `lint-v2.log`,
`lint-v4.log`, review records and the final source gate. QA PostgreSQL dumps and
logs were archived before cleanup. All created QA containers/volumes were
removed without OOM; primary image/configuration/domain checks remained intact.
The index remained empty. No commit, push or primary image install occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
