# Accepted message recovery after its target run finishes

Status: implemented, independently reviewed and verified with PostgreSQL-backed
restart tests. All 261 integration tests and the remaining Go packages passed on
Linux with the race detector. The corrected full image passed isolated runtime
acceptance. The main owned test stack was restored with preserved configuration and business
data; ingress completed a fresh successful Telegram polling cycle.

## Recovery contract

A saved control payload records intent. It does not prove that Loom accepted the
Input: the bot can stop between saving the payload and sending the request, or
lose the response after Loom commits it. Finishing the parent run does not resolve
this uncertainty.

When an ordinary accepted message has a frozen, uncertain Input and its target
has finished, the bot replays the exact payload to the same run with the accepted
task UUID as the idempotency key. It uses fresh user delegation under the same
issuer, subject, service actor and organization. This avoids relying on the
terminal parent's expiring auth session or changing its checkpoint.

A successful receipt proves acceptance, not successful execution. The control
finishes locally without an invented no-active reply or a replacement run.
Only HTTP 409 with structured error code `run_terminal` permits the existing
atomic promotion into a new run. The accepted task UUID and request are retained.
Generic conflicts, missing resources, service failures and lost responses preserve
the frozen intent in `awaiting_service`. A later unrelated rejection cannot
cancel an earlier uncertain effect. Current IAM denial/revocation gates still
apply before recovery.

This depends on Loom checking the retained idempotency record before terminal
state, and committing the input and its receipt together. Both properties were
verified in the owned `loom-upstream-1050` source copy. Its idempotency scope does
not include the auth session ID. Receipt retention must cover the entire unresolved
retry lifetime; destructive restore or receipt purging invalidates the absence
proof. Keep the service actor ID stable while work is unresolved; credential
rotation for the same identity is supported, but replacing the service identity
requires a drained queue or a separate reviewed recovery migration. No Loom checkout or stand was changed.

## Verification and limits

The native HTTP-client test accepts only a typed terminal conflict as negative
proof. Other conflict codes, generic responses, malformed/trailing JSON, 401 and
503 cannot authorize promotion. Error strings do not expose the response message
or user token. Native tests of `internal/loom` and `internal/task` pass on Windows
Go 1.27.2. Three independent source reviews found no actionable findings. Native scoped
`golangci-lint` reports zero issues after fixing the initial seven findings; the
first lint failure is retained locally.

The PostgreSQL restart scenarios passed:

- A cancel-aware HTTP barrier holds the request after checkpoint persistence and
  before its effect. After bot stop and parent completion, typed terminal replay
  must preserve the accepted message by starting its own run.
- The same restart with an unrelated conflict must remain unresolved, with the
  original UUID, payload and request and no duplicate run or invented reply.
- An applied Input whose receipt is lost must reconcile through the same key and
  frozen body after restart, with exactly one effect and no replacement run.

The mock now stores Input receipts before rejecting terminal runs and records
attempts separately from effects. The old handler must acknowledge cancellation
before restart, so the test cannot accidentally apply a request after shutdown.
The existing unknown-then-rejected test expects a reconciled acceptance without
a second artificial reply.

The prepared-request scenario first failed against the exact old engine, then
passed with the fix. The complete 261-test integration inventory and the explicit
command/internal/migration packages passed under Linux/race. Scoped native lint
passed after correcting the initial findings and a later test-file line-ending
issue; the original failure logs are retained.

The corrected full Dockerfile image also passed license/CA/binary inspection,
avatar rendering and isolated fresh-schema startup with real IAM and synthetic
Loom/Telegram peers. Two actual SIGTERM/restart observations recovered the same
task and exited normally in approximately 431 and 377 ms. A separate real-IAM/Loom probe also verified retained Input replay across
session-to-fresh-subject authentication and typed terminal rejection with zero
or one persisted input. Its initial cancellation returned 503; recovery continued
on the same recorded run and passed. These checks do not establish the whole-bot
worst-case shutdown SLA, expired-session runtime acceptance, unattended agent
behavior or production readiness. See [QA evidence and limits](shuttle-message-recovery-qa-2026-10-15.md).

Existing task-closure tools do not accept an unresolved Input alone as closure
evidence; they remain limited to their documented auth and outbox uncertainty.
No unsafe manual retry workaround is introduced. No SQL migration or new task
state is required. No commit, push or release was produced for this slice.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
