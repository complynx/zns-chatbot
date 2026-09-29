# Stage 1 validation — 2026-09-25

Scope: the Go infrastructure and shared-interaction sandbox described in
[the migration plan](go-migration.md). Full production behavior is not ported yet.

## Independent review loop

Three fresh, no-history static reviews inspected the stage-1 textual diff.

1. Two valid findings: unbounded Telegram polling response and unbounded encoded
   model context. Added batching/context budgets and focused tests.
2. One valid refinement: a count-only Telegram batch can still exceed the response
   limit when JSON escapes characters. Bound the serialized byte count and changed
   the backlog test to escaped input.
3. No actionable findings.

All reviewers were read-only. No fixes were staged. The staged diff was empty
before and after the review loop. No commit, push, PR or production change was made.

Two further independent Senior QA passes used only user requirements and the
running sandbox, without implementation context. Pass 1 found contradictory
permission-denied and booked-state text. It also identified unclear scripted
cancel and sold-out guidance. These were reproduced, fixed and covered by focused
tests. Fresh pass 2 found no new reproducible defects and accepted progression to
the next intermediate stage. Reports and HTTP evidence are in `qa.local/pass1`
and `qa.local/pass2`. These QA agents had no browser available. The implementation
agent reran the Edge mouse/touch suite after the fixes; it passed. This closes the
stage-1 gate, not the full migration or production-release gate.

## Executed checks

- Windows Go build/test/vet and module verification.
- Real PostgreSQL 17 integration tests, each in its own temporary database.
- Linux `go test -race -count=1 ./...` against PostgreSQL with final source mounted
  read-only into a dedicated test container.
- Identity fuzzing for five seconds, approximately 990,000 executions, no failure.
- Live Compose smoke: fake Telegram → polling bot → API → PostgreSQL; manual
  selection → agent continuation → confirmation → same-message update → cancel.
- Repeated live smoke after restarting the bot and fake Telegram; state persisted.
- Playwright browser suite with headless Edge: mouse and emulated touch, separate
  desktop/mobile viewports, manual/agent workflow, 429 retry, denied access for both
  paths, no page errors or horizontal page overflow. Screenshots inspected.
- OpenAI HTTP contract fixtures: exact `gpt-6-luna`, Responses JSON schema, scoped
  history, no response storage, refusal/incomplete/hostile/invalid output handling.

The live paid OpenAI endpoint and real Zitadel have not been exercised in this
stage. Browser checks used emulated touch, not touch hardware. CI configuration is
present but has not run on GitHub because the branch has not been pushed.

## Remaining release gates

Stages 2–4 in the migration plan remain open: actual orders/payment/pass/massage
parity, administrative and web functions, production identity, knowledge refresh,
quotas and removable forward importer. Current fixture coverage does not establish
production parity. Do not replace the Python production deployment with stage 1.
