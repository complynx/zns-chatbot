# Agent registration tools

The `zinusya_registrations` module  advertises two
owner-scoped methods. Both derive the local user from delegated IAM credentials.
The explicit event UUID must be resolved before calling; No recipient override is accepted.

- Read: `modules.zinusya_registrations.me({event_id})` through Loom's script tool.
  Returns the owner's current registration and optional assigned pass. A missing
  registration returns HTTP 404. It does not grant a pass or record payment.
- Write: model function `module_call({module:"zinusya_registrations",method:"join",
  arguments:{event_id,role:"leader"}})`. `role` accepts `leader` or `follower`.
  It requests a solo waitlist registration only on an explicit user request.
  Calling a write through the JavaScript proxy is unsupported by Loom.

`join` uses confirmation mode `none`, stable idempotency and a status path; this
reversible waitlist request has no payment or pass-allocation effect.
The advertised
automatic deduplication window is 24 hours; bot receipts persist without automatic
deletion.
Expiry of the broker window is not permission to retry under a new key.
Administrator assignment now has a separate permission-filtered exact-confirmation
method; see [the assignment contract](shuttle-agent-assignments.md).
The operator
API is not advertised as an agent write.
See the fixture boundary below.

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
