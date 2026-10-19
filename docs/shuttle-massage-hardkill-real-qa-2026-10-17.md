# Real Mini App precommit bot SIGKILL acceptance — 2026-10-17

Accepted scope: cancellation of one synthetic massage booking through the actual
Telegram Mini App, SIGKILL of the full owned bot before transaction commit,
rollback, restart of the same container, and explicit retry of the original
request. This is a cancellation-boundary result, not full massage or production
acceptance. No Go runtime code changed for this gate.

## Environment and fixture

Only `shuttle-live-test` and the authorized existing account/chat
`@complynx_test_bot` were used. The bot image stayed `904344b4`, schema46 /
manifest `mvp-49`; own Loom, IAM and PostgreSQL were not restarted. The separate
Loom developer environment and production were not changed.

Event `74244b1f-dc3c-452f-8e7d-6e6a083b6323` uses `Europe/Berlin`. The target
booking `3b81f4ed-1f2f-43ea-b072-b3455327be2b` was booked/v1 for 25 October,
01:00 GMT+02:00. Its synthetic price is 45.00 BYN; this test moved no money.
The independent 03:00 GMT+01:00 booking remained unchanged.

A bounded loopback recording proxy used 128 MiB with no extra swap. Temporary
origin/launch settings routed the real embedded app through port18383; the bot
itself kept its ports, key, image and security settings. A private database backup
and data baselines were captured before cutover. A second owner/event baseline
was captured after the fresh native cancellation preview, before confirmation.

## Fault and observed recovery

1. The native `/massage` command completed as one durable task without a Loom
   run. The actual form selected the target active booking and displayed its
   cancellation preview and original expiry.
2. A randomly named test trigger matched only this owner, event, operation,
   proposal, booking, cancellation source and expected cancelled/v2 result. It
   acquired a transaction advisory marker and slept at receipt insertion, after
   booking and proposal mutation but before commit. Both trigger WHEN and body
   carried the same guard. The 90-second sleep did not extend the real
   five-second application request deadline.
3. The prearmed controller observed exactly one marker with `PgSleep`, recorded
   PID/backend_start/query_start, and killed the exact bot container. Docker
   confirmed exit137, no OOM and no automatic restart. FinishedAt was 0.511057854
   seconds after the proxy recorded the actual POST start, within the stricter
   three-second gate. Only after that proof did the controller cancel the exact
   still-sleeping backend and wait for that backend identity to disappear.
4. The recorder observed `RemoteDisconnected` before any upstream HTTP status
   and closed its downstream connection without a synthetic response. The form
   displayed an unknown result, disabled event/specialist changes, hid confirm
   and discard, and offered only recovery or retry of the existing request.
5. Database reads proved the target remained booked/v1, the original proposal
   remained unconsumed and no cancellation receipt existed. All other massage
   rows across users/events matched the baseline. The exact test helpers were
   removed, and the same bot container was started with unchanged business hashes.
6. The actual form's Check earlier request sent GET for the original operation
   under a phase armed only after restart readiness. It returned404; the form
   kept the unknown state and blocked a different action.
7. Retry the same request sent the identical raw-body SHA256, operation ID and
   proposal ID. It returned200/cancelled/v2 before the original proposal expiry.
   There was one cancellation receipt and one consumed original proposal, with
   no replacement consent. The form showed Cancelled, enabled event selection
   and hid recovery/retry controls.

Operation: `9adf45a0-1f42-40c2-99d6-41e314d04151`.
Proposal: `d9f6fb38-cf20-4e93-8a19-374d393fae3e`.
Proposal expiry: `2026-10-17T17:44:52.982861Z`; cancellation receipt was created
at `2026-10-17T17:42:49.110832Z`. Request SHA256:
`668f3a45e2525d9b700dde35e06f08f45697a7c20cd7e06606404962cb01f099`.

Task/run counts were177/118 before the second native command and178/118 after
it. Form preparation, failed apply, recovery lookup and retry added no further
task or Loom run. Every unrelated massage booking, preparation and operation
across users/events was checked globally. The allowed booking changed only
state/version/cancelled_at; proposal changes were limited to consumption fields.
Cross-domain table hashes were checked around route changes, not claimed as a
separate cross-domain apply-isolation test.

## Preserved failures and review

The first live attempt stopped during arm: the controller marker SELECT had an
extra closing parenthesis. Its fake-peer tests and static reviews had missed the
SQL syntax. No Confirm was sent and no bot was killed. The exact installed test
trigger/function were removed and original routing/ingress restored. Its failed
checkpoint, source copies and unused proposal remain historical evidence.

After the fix, a real PostgreSQL adapter fixture verified both marker-query paths,
the full64-bit transaction advisory key during PgSleep, and backend identity
across query changes/session exit. It used only its own session, SELECT and
transaction advisory locks. The driver now requires that receipt before cutover.

Recorder reviews corrected transport classification, incomplete-body handling and
retry eligibility. Controller review corrected query disappearance being confused
with backend disappearance. Driver review expanded preservation checks from one
owner/event to all unrelated massage rows. Fresh final independent reviews found
no actionable findings. Mock checks, real adapter checks and actual Mini App
acceptance remain separately scoped; no model inference was used by this gate.

## Cleanup and evidence

The actual form was closed, original port18382 origin/launch settings restored,
exact proxy removed, same poller resumed, and the eight companion fingerprints
and required counted provider preserved. SQL helpers were removed before restart.
Route-restoration hashes passed; the synthetic target remains cancelled/v2 with
its immutable history. No commit, push, PR, issue or production action occurred.

Local ignored evidence:

- `.cache/shuttle-live-test/massage-hardkill-qa/checkpoint.json`: failed first arm,
  `cleanup_completed`, passed=false.
- `.cache/shuttle-live-test/massage-hardkill-qa-v2/checkpoint.json`: accepted and
  cleaned; controller/proxy receipts, backup and row baselines are alongside it.
- `.cache/shuttle-work/massage-hardkill-proxy-unit-v4/checkpoint.json` and
  `massage-hardkill-adversarial-v1/checkpoint.json`: recorder mock checks.
- `.cache/shuttle-work/massage-hardkill-controller-unit-v3/checkpoint.json`:
  controller fake-peer command-order/failure checks.
- `.cache/shuttle-work/massage-hardkill-postgres-adapter-v1/checkpoint.json`:
  actual PostgreSQL adapter checks.

Authorization, initData and raw request bodies were not recorded by the proxy.
Private backups and existing proof files stay ignored and are not published.

## Remaining gates

The separate [committed booking response-loss/clean-restart gate](shuttle-massage-response-loss-real-qa-2026-10-17.md)
and [store subprocess before/after-commit gate](shuttle-massage-process-crash-qa-2026-10-17.md)
retain their own scopes. This result adds actual full-bot precommit cancellation
SIGKILL recovery. Full-bot booking/after-commit kill boundaries, agent/mixed
lost-response handoffs, combined Loom/IAM outages, specialist/admin lifecycle,
notifications, remaining Python parity and unattended model behavior remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
