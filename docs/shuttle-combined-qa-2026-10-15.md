# Combined Go verification — 2026-10-15

All 251 enumerated top-level integration tests and the other explicit Go rewrite
packages passed under the Linux race detector. Tests ran in bounded groups, not
as one simultaneous suite. This report retains the earlier OOM and clock failures.

## Retained failures

The first combined Linux race command covered explicit Go rewrite packages:
`./cmd/... ./internal/... ./migrations/... ./integration`. Nonintegration packages
passed, some from cache. The integration process was killed at 86.026 seconds.
Docker recorded `OOMKilled:true` and exit 1 under the 1.5 GiB test-container limit.
This is a failed run. It does not establish a production application memory leak.

The existing resource-bounded strategy was then reused: enumerate all top-level
integration tests and execute groups of at most fifteen in fresh containers.
Each uses a 256 MiB Go-runtime target, GOGC 25, two CPUs, a 1.5 GiB/no-swap container
limit, package concurrency one and test concurrency two. This soft target is not
a total process memory limit. Explicit other packages run separately.
The authoritative list contains 251 top-level integration tests across seventeen groups.

The first batched run stopped in group ten on
`TestPaymentNotificationRepairDoesNotStarveHealthyRecipientBehindWaitingPage`.
An owner request received HTTP 401 instead of 200. Earlier passing groups and the
failed plan remain unchanged; this attempt is not a complete passing suite.

## Clock diagnosis and fixture-only fix

Isolated repetitions reproduced the 401. A temporary diagnostic source overlay
localized it to signed JWT validation before introspection. The decisive failure
recorded issuance Unix time 1792081467 and validation Unix time 1792081465.
Signature/decode, organization, subject and expiry presence were valid;
validation reported a future `iat`. The same-process mock had issued the token
from its current wall clock, establishing a backward clock movement of at least
two seconds. No token or key material was logged.

Only the generic integration IAM fixture changed: one captured time supplies
issuance one minute earlier and expiry one hour later. These scenarios do not
test issuance-time boundaries. This margin tolerates the observed clock step;
it is not a guarantee against arbitrary clock changes. Production JWT validation,
expiry checks, revocation and clock leeway remain unchanged.

A separate public Verify behavior test signs an otherwise valid token issued one
minute in the future. It must be denied before introspection and without cache
activity. Five focused repetitions passed: identity 1.041 seconds, integration
43.762 seconds. Scoped identity/integration lint reported zero issues.

Fresh independent correctness and adversarial quality reviews were checked against
the source and configured toolchain. No valid substantive finding remained. One
review claimed the new claims literal could not compile; actual required Go 1.27.2
compilation, race execution and lint accepted it, as with the existing fixture's
same literal style. No edit was made for that false positive.

## Current complete-run gate

After recreating the disposable QA PostgreSQL, a new plan started coverage from
zero against a frozen source tree. All 251 names appear exactly once in its
seventeen completed groups. The listing container, explicit-package container and
all seventeen integration containers exited 0 without OOM and were removed.
All 329 frozen source-path hashes still matched after the run. The earlier failed
plans were not overwritten. This proves complete listed coverage in groups; it
does not certify the memory use or timing of one combined integration process.

The disposable QA database and its anonymous volume were removed after test
completion. Its final teardown recorded exit 137 with `OOMKilled:false`; this is
not accepted as a graceful database shutdown. It is separate from the nineteen
successful test containers and from the unchanged bot/runtime shutdown contract.

## Scope and evidence

Only two test files differ from the accepted cloud-file runtime source freeze.
All production Go/SQL/configuration/dependency paths are unchanged. The owned bot
remains on image
`sha256:315269a74e66c9d4129d428dfabefcf9c8ab35afb4e95aae095d98f445c93bd2`,
schema 31 / manifest `mvp-30`. No new runtime build, service restart, Telegram
message or IAM/business mutation was needed for this fixture fix.

Ignored evidence is under `.cache/shuttle-work/combined-qa/`,
`.cache/shuttle-work/notice-auth-qa/` and `.cache/shuttle-work/container-results/`.
The current coverage plan is
`zinusya-combined-clock-final-integration-batch-plan-20261015.json`; its
predecessor `zinusya-combined-final-integration-batch-plan-20261015.json` retains
the group-ten failure. The single-process OOM evidence uses prefix
`zinusya-combined-full-qa-20261015`. Diagnosis logs use `zinusya-notice-auth-*`.

This is local source verification, not a new GitHub CI run or full native
functional acceptance. Browser CA/zitadeltg, remaining business-policy decisions,
real migration, full shutdown SLA and full causal tracing remain separate gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
