# Order notice worker process recovery — 2026-10-17

## Result and scope

The worker-process crash/replay component gate passes with Go 1.27.2, PostgreSQL 17
and mock Telegram/IAM/Loom HTTP peers. The production Go/SQL code is unchanged.
One new behavior test starts the real task service in a separate process using
the parent's isolated schema. It does not run the complete application executable,
real Telegram, real IAM or the owned Loom provider. Full application graceful-stop
latency, upstream outage acceptance and mixed browser paths remain separate.

## Verified behavior

The mock Telegram peer records one incoming send and blocks its HTTP response.
The notice has already persisted send_inflight and its attempt ID. The test
force-kills that child worker before releasing the mock response, then verifies
that the uncertain attempt remains durable and its database sessions are closed.

Starting a new service against the same schema quarantines that notice as
delivery_unknown with the original attempt and business snapshot. The test waits
up to 90 seconds for the exact crashed River job to complete with a higher attempt
count. It does not manually alter the queue or invoke recovery methods directly.
After that real replay, the notice is unchanged, only one send was observed,
resource holds are unchanged, no payment review exists and no Loom run was created.
The notice requires explicit administrator resolution before another send.

The final gate includes the existing pending-work restart/deduplicated-recipient
path and authorized unknown-delivery retry/dismiss paths. Three roots and five
pass events complete under race detection, with no skips, failures or OOM; all 938
frozen inputs remain unchanged. Integration lint reports zero issues. Fresh
independent source review reported no actionable findings. The exact disposable
Go/PostgreSQL containers and their owned volume were removed; the main ten Compose
services stayed unchanged. The separate counted fixture was not a QA target.

## Retained attempts

The first run found a test-hook data race. Synchronizing the hook setup fixed it.
The next green run proved startup quarantine only. Review found that its one-second
observation could end before queue rescue, so that result does not establish
crashed-job replay. The strengthened test now observes the exact job's completion
before checking delivery and business invariants. Formatting and local error-name
lint failures are retained; the final gate uses the lint-clean source.

Ignored receipts are in `.cache/shuttle-work/order-notice-process-v1-qa` through
`order-notice-process-v5-qa`. The final v5 checkpoint is accepted_and_cleaned, with
its frozen source list, race JSON events and zero-issue lint receipt. No production
readiness, deployment, publication or real-model acceptance is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
