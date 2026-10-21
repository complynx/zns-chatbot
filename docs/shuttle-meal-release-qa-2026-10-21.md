# Meal financial release verification, 2026-10-21

Status: all application tests pass on the final source. The original test
controller exits1 during fixture cleanup; that failure remains retained.
Separate verified cleanup retires its resources. The primary ran
schema68/mvp-86 at that gate. Subsequent remote CI and
[primary installation](shuttle-meal-primary-qa-2026-10-21.md) are now accepted
separately. The original cleanup failure remains unchanged.

## Application verification

The Linux/amd64 Go1.27.2 race run passes864 integration roots /1293 cases in44
bounded sequential batches against PostgreSQL17. Every discovered root runs
exactly once; no integration failure or skip is accepted. Unit/race tests also
pass. Their sole allowed helper skip is cmd/shuttle/TestWatchdogHelper.

The host gate passes367 UI cases with no failures, skips or cancellations,
strict lint, dependency checks, module verification, simulator/guard tests and
pinned sqlc compilation/regeneration. Generated SQL matches the working source.
The operational rules/dashboard checks reuse exact unchanged input hashes from
the accepted preceding check.

The unchanged release Dockerfile builds image
`sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69`.
Its3GiB builder is removed. The preceding1.5GiB compiler OOM and its logs remain
retained; that failed builder is also removed. No image is published.

The stale-draft behavioral fix is included: event and catalog changes return409
rather than503, followed by a successful resave and explicit submission. All
three root/subcase results are present in batch20. The independent source,
quality and focused fix reviews are recorded in the release evidence.

## Retained fixture-cleanup failure

After all864 roots passed, the controller exported the default test database
and fully decoded its1057-byte custom-format dump. Test schemas had been removed.
It then requested docker stop with a10-second limit. PostgreSQL logged a fast
shutdown request, but exited137 about10 seconds later; OOMKilled is false and
there is no completed-shutdown log. The observations are consistent with the
stop budget expiring; the underlying shutdown delay was not established.

The controller's exit1 and original checkpoint remain unchanged. No tests were
rerun and the original database container was not restarted. A distinct cleanup
procedure saved its logs/state, verified ownership, decoded the same dump again
in a96MiB network-isolated decoder, then removed the decoder and the already
stopped PostgreSQL container, volume and network. Current label-based checks
confirm that no resources owned by this test run remain.

This is an application-test acceptance with a documented cleanup exception,
not a claim that the original orchestration process completed successfully.
PostgreSQL restart/graceful-shutdown acceptance is outside this application gate.

A separate read-only acceptance check also initially used the wrong host JSON
field, jobs instead of checks. It failed before runtime changes or an acceptance
artifact. The corrected checker reads the retained logs and actual input schema;
it does not repeat tests or cleanup. This diagnostic failure is retained too.

## Preserved state and remaining work

Source hashes match the tested/build inputs. The owned primary's domain,
database identity and eleven-service runtime remain preserved. The
[migration-copy rehearsal](shuttle-meal-schema69-rehearsal-2026-10-21.md) and
[bounded financial runtime QA](shuttle-meal-financial-runtime-qa-2026-10-21.md)
remain separate evidence: neither claims this candidate is installed.

Private evidence is under .cache/shuttle-work/meal-financial-flow-v1/release-v2/:
race-checkpoint.json and batch logs retain the original execution;
retirement-resume.json records cleanup; acceptance.json checks all864 roots,
1293 cases, source/build hashes and actual resource retirement. The original
failure copy and PostgreSQL logs are retained alongside these records.

The subsequent reviewed commit, remote CI and controlled owned-primary upgrade
are recorded in the linked installation report. Deferred joint CA/ZitadelTG/real Telegram checks,
exact native-operation adoption and durable causal tracing through Loom remain
open roadmap gates. No production cutover or readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
