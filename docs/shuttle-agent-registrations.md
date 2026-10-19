# Agent registration tools

The `zinusya_registrations` module (introduced in `mvp-4`; current manifest revision `mvp-8`) advertises two
owner-scoped methods. Both derive the local user from delegated IAM credentials.
The explicit event UUID must be resolved before calling; context selection is
subsequent work. No recipient override is accepted.

- Read: `modules.zinusya_registrations.me({event_id})` through Loom's script tool.
  Returns the owner's current registration and optional assigned pass. A missing
  registration returns HTTP 404. It does not grant a pass or record payment.
- Write: model function `module_call({module:"zinusya_registrations",method:"join",
  arguments:{event_id,role:"leader"}})`. `role` accepts `leader` or `follower`.
  It requests a solo waitlist registration only on an explicit user request.
  Calling a write through the JavaScript proxy is unsupported by Loom.

`join` uses confirmation mode `none`, stable idempotency and a status path; this
reversible waitlist request has no payment or pass-allocation effect. The advertised
automatic deduplication window is 24 hours; bot receipts persist without automatic
deletion. Expiry of the broker window is not permission to retry under a new key.
Administrator assignment now has a separate permission-filtered exact-confirmation
method; see [the assignment contract](shuttle-agent-assignments.md). The operator
API is not advertised as an agent write. Native broker acceptance passed on the isolated upstream working seal fixture;
Telegram UI acceptance remains pending. See the fixture boundary below.

## Wire protocol

`POST /internal/agent/registrations/join` accepts `protocol_version:1`,
`operation_id`, optional `run_id`, `arguments`, `idempotency_key` matching the
operation ID, and optional confirmation object with `mode:"none"`.
An execution returns HTTP 200 with `status:"succeeded"`, the same operation ID
and a registration in `result`. Proven business rejections return HTTP 200 with
`status:"rejected"`, the operation ID and structured `error` with
`classification:"terminal_rejection"`. They do not masquerade as transport errors.
Storage/commit errors remain uncertain; authorization failures retain HTTP errors.
The output schema specifies UUIDs, role/state, positive version, UTC instants and
optional pass with exact integer price, currency and scale.

`POST /internal/agent/registrations/status` accepts `protocol_version:1` and
`operation_id`. It returns `succeeded` with the immutable original join result or
`not_found` without a result. Status excludes other actors, deployments and action
kinds, including administrator assignment receipts. `not_found` is not a promise
that a delayed execution cannot commit. Current Loom correctly treats it as unknown;
reconcile the same operation rather than blindly replacing it.

`POST /internal/agent/registrations/me` uses the ordinary read envelope with
`arguments:{event_id}`. Read results reflect current state; join status returns
the original result even after assignment or an event edit.

## Acceptance boundary

Behavioral HTTP/PostgreSQL tests prove restart reconciliation, exact retries,
owner-isolated status/read, key mismatch, changed-argument rejection and exclusion
of assignment receipts. Two fresh textual review passes: the first found HTTP
business failures becoming unknown in Loom; the adapter now returns the explicit
rejected envelope and the second pass found no actionable findings.

Native broker acceptance on the unchanged owned Loom image **failed before
Shuttle dispatch**. Persisted JSONB reorders raw argument keys and changes Loom's
operation binding seal; the write remains approved and the model sees tool_failed.
Read-only reproduction: the loaded seal differs, and canonicalizing the argument
objects reproduces the original seal. This is reported in
[Loom issue 3](https://github.com/complynx/loom/issues/3).
The failure remains recorded in the ignored `loom-registration-failure.json`.
No upstream developer checkout or stand was changed. A failure response was supplied to the isolated provider scenario, but the native
run ultimately remained paused with `platform_retry_limit`; its write remains
approved and unexecuted. It was not blindly retried or reclassified as successful.

Direct real-IAM wire acceptance passed on 2026-10-13: join, read, successful status,
exact retry, explicit rejected conflict and unknown status. Evidence is ignored
`.cache/shuttle-live-test/agent-registration-direct-proof.json`. Final app image:
`sha256:4259300804bf08d512148a7918ac87e0b0f09025cec73345d7709406b0f2caad`.
Affected production packages and the complete PostgreSQL integration suite passed
Linux race checks after the rejection fix (integration 28.979 s); linter: zero issues.
The previous solo persistence slice also passed all production-package race tests.
Existing users/tasks/outbox counts remained 2/8/8; staged diff remained empty.
Successful direct API tests do not imply a successful native broker write.

Subsequent native broker acceptance passed on 2026-10-13 with real delegated IAM
and the [isolated upstream working seal fixture](shuttle-loom-seal-fixture.md).
A new run completed owner join, durable event choice and a context script read;
the original failed run remains unchanged. This does not certify the upstream
committed release or the Telegram UI gate.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
