# Selected model interruption controls

This control belongs to the synthetic provider. It adds no business mutation,
plan replay, receipt, authorization or scheduling rule. Use the existing fixture
provider with its existing private `R104_CONTROL_KEY`, reserved control listener
on port 8090 and exclusive `R104_JOURNAL`. The normal data listener and model
fixture route remain unchanged. No separate model listener is required.

## Setup and readback

Install the ordinary typed model fixture through `POST /lab/model/fixtures`.
Keep `X-Sandbox: 1`. A selected interruption additionally requires the private
`X-R104-Control` header and this optional field:

```json
{"hold":{"turn":0,"mode":"after_consume"}}
```

`before_consume` and `after_consume` are the only modes. The zero-based turn must
identify a step in this installation. Existing synthetic owners are `alice`,
`bob` and `visitor`. An explicit owner/update installation retains its original
scope. An installation with `input` receives its actual owner/update through
the existing enqueue operation before the bot can poll it. Retain that returned
update identity privately; do not invent or reuse another identity.

A request must match the actual scoped turn and expected typed model input
before it can select the hold. The hold applies once. Other owners/updates do
not wait for it. Concurrent calls cannot consume the same turn twice. No
controller lock is held during a wait, HTTP response or SQL operation.

The authenticated reserved listener accepts:

* `GET /control/model/state?owner=alice&update_id=9&turn=0`
* `POST /control/model/release?owner=alice&update_id=9&turn=0`, with JSON
  `{"action":"deliver"}`, `{"action":"fail"}` or `{"action":"disconnect"}`.

Every control request needs `X-R104-Control`. The control body limit remains
1 KiB and the existing connection/header/read/write bounds remain active.
Only an actual held phase can be released; release is one-shot. Invalid scope,
unavailable scope and non-held scope return 400, 404 and 409 respectively.
`fail` returns a static unavailable response. `disconnect` closes the selected
model response without returning the generated plan. Neither action reinstalls
the step. `deliver` permits the original generated response to continue.

Readback contains exact synthetic owner/update/turn, hold mode, phase, consumed
and durable flags, and SHA256 request/response digests when consumed. It contains
no model input, plan, script, credential or provider error. The operator keeps
these exact identities and hashes privately and publishes opaque case tokens
and status/count evidence to the source-blind reviewer.

## Phase meaning and limits

`held_before_consume` means the matched request has not consumed its step.
`persistence_pending` means the step was consumed but the provider-state write
has not been confirmed; release is forbidden. `consumed_held` with `durable:true`
means the provider recorded consumption in its existing `bot.fake_state` and
has not returned the plan. `held_process_local` with `durable:false` is only an
in-memory hold when no database was supplied; it cannot qualify durable evidence.

The consumption write and both provider/fixture lock acquisitions share a
2-second deadline. Selection has its own 2-second admission bound. HTTP state
responses copy under the fixture lock and write after unlocking. Model work
uses both request and provider lifetimes, including while waiting for locks or
SQL. Observable cancellation takes priority over release success and is checked
again at consumption, persistence and response delivery. SQL failure produces
`persistence_unavailable`, `consumed:true`,
`durable:false`; it never rewinds or acknowledges a durable hold. This is unknown
persistence, not proof that SQL did not commit. The hold ends on release, request
cancellation, provider lifetime cancellation or 10 seconds. The existing model
client also bounds its request to 10 seconds; expiry can race client cancellation.
Observe the actual terminal phase instead of assuming one timeout winner.

Terminal phases include `request_cancelled`, `provider_stopped`, `hold_expired`,
`provider_failure`, `response_generated`, `setup_unavailable`, and
`consumed_unavailable`. `response_generated` describes only provider response
generation. It does not prove HTTP receipt, durable business plan save or effect.

At most 32 consumed turns and 32 control identities are retained per provider
state. Capacity exhaustion fails visibly before another turn is consumed; it
does not evict a tombstone. Restart restores only versioned consumption metadata
and tombstones. It does not restore fixture expectations, plans or holds. Installing
any fixture for an already consumed owner/update is rejected, including after
restart. A new identity can be installed for a genuine subsequent intent while
capacity remains. Unconsumed fixture bodies do not survive restart either.

## Lead-owned controlled provider reboot

Use one writer and an explicitly controlled process/container replacement:

1. Record the exact private owner/update/turn and actual `consumed_held` durable
   readback while the response is held. Coordinate the separate qualified
   business saved-plan/effect observer in this live window.
2. Stop the old provider and wait for its listener/process to stop. Preserve its
   private journal file and SHA256 hash. Do not append, truncate or remove it.
3. Start the replacement with the same provider-state database/data, case,
   consumed tombstones, control key, model configuration and product database.
   Select a fresh generation-specific private `R104_JOURNAL` path. Record old/new
   opaque generation identities and each private journal hash. This path change
   is operator evidence custody; it is not fixture reinstall or model-step reset.
4. Read the same exact scope: confirmed metadata becomes `consumed_unavailable`
   with `durable:true`. Verify old fixture installation and plan replay are denied.
   Do not reinsert the consumed response, inject a winner or reset the case.
5. Observe the real bot UI/draft and durable state, then execute a genuine
   subsequent intent through the normal agent/manual flow.

The existing journal uses exclusive creation. An ordinary restart with the
identical old journal path fails intentionally. This procedure does not claim
unchanged-environment Docker restart support or acceptance for other lifecycle
cases. Default existing R104 cases and journal protection remain unchanged.

## Acceptance boundary and local gates

Provider tests cover actual HTTP setup/authentication/release, concurrent
consumption, cancellation, failures, bounded SQL admission, native reserved
listener startup, exclusive journal custody, database persistence and reopening.
The serial PostgreSQL selector is:

```text
go test -mod=readonly -count=1 -parallel=1 ./integration -run '^TestModelConsumption(DurableRebootAndDeniedReinstallation|SQLDeadlineDoesNotConfirmOrRewind)$'
```

These tests use the existing integration helper to create, migrate, seed and
drop random disposable child databases through `TEST_DATABASE_URL`. They require
an exclusively allocated PostgreSQL gate and port 8090; run no parallel environment
or listener tests. Offline sandbox units include a real native listener restart
but cannot certify database persistence. An integration compile is not a PG pass.

Full F08 acceptance remains pending the independently qualified saved-plan/effect
observer and the actual source-blind functional execution. Required outcomes are
bounded termination, no unauthorized or duplicate mutation, a visible recoverable
outcome, a working subsequent intent and coherent durable completed state/draft.
`consumed_unavailable` alone does not establish any of these business outcomes.

Written by d_observability_developer (gpt-6/Codex)
on behalf of Daniel Drizhuk
