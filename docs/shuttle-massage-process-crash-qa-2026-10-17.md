# Massage transaction-boundary process crash QA, 2026-10-17

This accepts four store-boundary process-kill cases with real PostgreSQL.
It does not accept a full bot executable crash or lost Mini App HTTP response.
Production code, migrations and runtime image were not changed by this slice.

## Failure boundaries and observed recovery

The test starts a separate instance of the integration test executable against
one fixture's isolated random schema and existing prepared operation. The child
uses the production massage store methods. PostgreSQL remains running throughout.
Booking and cancellation each run at two boundaries:

- Before commit: a schema-local test trigger blocks receipt insertion after the
  business write and proposal consumption inside the same transaction. The parent
  observes the child's actual PostgreSQL advisory-lock wait, kills and reaps the
  process, releases the fault latch and waits for its database sessions to vanish.
  No client remains to send COMMIT. A fresh store sees no operation receipt and
  either no booking or the unchanged booked record for cancellation. Replay of
  the original operation/proposal then completes once.
- After commit: the child signals readiness only after the production store call
  returns successfully. It is killed before any result is supplied to a simulated
  caller. A fresh store reads the durable original receipt; replay returns that
  identical result without another effect.

All four cases repeat the same operation again and check persisted current state,
one receipt for that operation and exactly one booking record. Booking ends
booked; cancellation ends cancelled. These checks establish transactional rollback
and committed receipt recovery, not transport or unattended model behavior.

The fault key derives from the operation UUID, so parallel schemas do not share a
single latch. Trigger/function names live in each fixture's isolated search_path.
Cleanup kills/reaps any remaining child before unlocking, uses a fresh bounded
context and releases its dedicated connection afterward.

## Verification and preserved failures

The first run passed both after-commit cases but failed before-commit teardown.
The parent waited for the blocked PostgreSQL backend to disappear before releasing
its test latch. Cleanup also attempted unlock through t.Context(), which Go had
already cancelled. Both independent source reviewers identified the cancelled
cleanup context. The fault latch now releases immediately after process death;
cleanup uses a separate five-second context and an idempotent unlock.

The original failed run and its frozen inputs remain recorded. Its exact owned
containers and volume were removed before source changes. Initial lint runs also
caught IPC output, JSON-tag and parallel-root issues; they were corrected without
production type changes or linter exclusions.

The final unchanged-source Linux/Go1.27.2/PostgreSQL17 race gate passed four roots
and sixteen pass events, zero failures and zero skips. It includes the four crash
cases, canonical approval/reply/restart cases, historical receipt reuse across
interfaces and single-connection Mini App handoff/replay. Configured lint passed
with zero issues. Fresh correctness and skeptical quality reviewers found no
actionable findings after the fixes. Git index remained empty.

Disposable PostgreSQL and Go containers had memory/no-swap and CPU limits. Neither
OOMed. The final PostgreSQL readiness check passed. Exact containers and the owned
test volume were removed; all ten main Compose service fingerprints were unchanged.
The current owned image remains904344b4, schema46 / manifest mvp-49.

## Remaining acceptance

The later [real Mini App lost-response gate](shuttle-massage-response-loss-real-qa-2026-10-17.md)
now accepts recovery of the original operation after a clean bot restart without
another effect.
The later [real Mini App precommit cancellation SIGKILL gate](shuttle-massage-hardkill-real-qa-2026-10-17.md)
now also passes. Other full-bot kill boundaries and Loom/IAM outage integration
remain separate gates. Specialist lifecycle and notifications remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
