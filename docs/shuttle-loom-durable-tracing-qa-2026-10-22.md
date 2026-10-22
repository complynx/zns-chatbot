# Loom durable tracing source acceptance — 2026-10-22

[Loom #27](https://github.com/complynx/loom/issues/27) is closed. Published main
`b2b8d429156adcb4d667cb950a6c0a8d97f09cce` passed
[CI](https://github.com/complynx/loom/actions/runs/39760617928).
Its tree `ac6123470bd2fdae10f20e9d34c9fb0564d3bfc0` matches our clean read-only
checkout at `8096604e5679a06b661f2b43cd7d42160276b068`.

## Independent local execution

The controller exported committed sources into an ignored directory and ran the
existing focused tests under Linux, Go1.27.3 and race, with real PostgreSQL/River
and an OTLP/HTTP receiver. External identity, provider and module peers were
synthetic. No OpenAI request, real Telegram message or production access occurred.
The sibling checkout and its developer stand were not modified.

All nine selected root tests passed, with zero failed or skipped events:

- `app.TestQueuedTraceSurvivesGracefulRestartAndSteering`
- `app.TestQueuedTraceGracefulProcessRestart`
- `app.TestQueuedTraceLegacyAndInvalidContextStillExecutes`
- `app.TestSteeringTraceChangesOnlyWhenInputsAreConsumed`
- `app.TestApprovalContinuationUsesConsumedTraceAndDoesNotRepeatEffect`
- `app.TestOrphanQueueSurvivesSIGKILL`
- `telemetry.TestDurableTraceValidation`
- `store.TestTraceContextAcceptanceReplayAndLease`
- `store.TestAppliedTraceIsMonotonicAcrossRestartAndDelayedInput`

These cover queued delivery after ingress, process SIGTERM restart, expired-lease
SIGKILL recovery, consumed-input steering, approval continuation without repeating
the effect, acceptance replay, delayed older inputs, and legacy/invalid carriers.
In-flight work retains its original context; subsequent consumed-input work uses
the new context and causal links. The upstream documentation defines the exact
parent and link policy; telemetry remains diagnostic and grants no authority.

The bounded runner exited0 without OOM. Its container, PostgreSQL container,
anonymous PostgreSQL volume and owned network were removed after retaining
results. The fixture used at most2GiB for the runner and512MiB for PostgreSQL.
Controller reviews corrected missing exact required-test coverage and added
hash-pinned published-tree/CI evidence before execution. Private receipts are in
`.cache/shuttle-work/loom27-acceptance-20261022-v1/`.

## Acceptance boundary

This is independently rerun upstream regression evidence, not full Zinusya
consumer acceptance. Installation of the published image on the owned primary
stand is accepted in the separate
[primary-upgrade report](shuttle-profile-name-sync-qa-2026-10-22.md#owned-primary-upgrade-with-ingress-fenced);
this upstream test did not perform that installation. Separate consumer gates now verify
[intake/task/queue/RPC, steering and bot restart](shuttle-consumer-tracing-qa-2026-10-22.md)
and [Loom restart](shuttle-consumer-loom-restart-tracing-qa-2026-10-22.md)
with original task/run IDs. The separate
[approval consumer gate](shuttle-consumer-approval-tracing-qa-2026-10-22.md)
also passes original approval/operation continuation across both restarts and
the exact successful commit HTTP span in the consumed-consent trace.
External trace-backend visibility, all crash windows and production rollout remain
separate. Earlier failed tracing/outage observations remain preserved.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
