# Contextual log acceptance — 2026-10-15

The application now correlates existing contextual JSON diagnostics with their
current OpenTelemetry trace/span IDs. Architecture and Go consultations preceded
the implementation. No logging SDK, global provider, access-log stream, dependency
or schema was added. See the [contract](shuttle-log-correlation.md).

## Source behavior and review

Tests decode actual JSON from `slog.Logger`. They cover valid sampled/unsampled
contexts, invalid-context omission, level filtering, existing attributes, derived
loggers/groups, concurrent context isolation and the executable's logger wiring.
A request through the actual telemetry middleware produces a diagnostic matching
its current server span, retaining the incoming trace ID and creating a different
span ID. Private path/tracestate canaries are absent from that output.

Linux race checks passed for `internal/observability` in 1.504 seconds and
`cmd/shuttle` in 1.026 seconds. The bounded runner exited 0 without OOM and was
removed. An earlier host command failed on sandbox cache/network access before
tests could run; it is not a passing gate. Strict lint initially rejected an
unused `nolint:testpackage` directive. Removing that comment produced zero issues;
no application behavior changed for this lint correction. A new full integration
or GitHub CI pass is not claimed.

Fresh independent correctness and adversarial quality reviews found no actionable
findings. They checked handler delegation, record cloning, derived groups,
concurrency, validity/sampling, privacy and application wiring.

## Owned executable check

The owned stand installed image
`sha256:db9ffd31b287ab23c50d2dac2fccac1f40accb38292bb764cbb629696afd96ad`.
The new Linux binary was built from the 326-path source freeze. Its compact
scratch QA image copied unchanged certificates, avatar executable and license
notices from the accepted predecessor; this is not a new full production
Dockerfile/CI acceptance. The build container had 512 MiB memory/no swap and no
network; it exited 0 without OOM and was removed. Only the owned bot was recreated;
Loom and other services were not restarted.

Two authenticated synthetic webhook fixtures used the authorized test account
and private-chat IDs. Their callback data was deliberately invalid, so the
existing parser rejected them before durable intake. The bot still attempted
acknowledgement of the intentionally invalid callback IDs and emitted its existing
`callback acknowledgement unavailable` warning. This checks the real executable
HTTP/context/logger path; it is not a real Telegram Web button interaction.
No task, Loom run or chat message was created.

The fixtures supplied incoming trace contexts with sampling flags `01` and `00`.
Each produced exactly one warning with the expected trace ID and a nonzero server
span ID different from the incoming parent. Injected callback/payload/tracestate
canaries and the webhook secret were absent from the inspected container logs.
The startup `application ready` record had neither correlation field. This is a
scoped privacy/context check, not an audit of every possible log attribute.
No OTLP export was inspected in this scenario; neither flag proves delivery to a
collector.

The first verifier used the host clock in Docker's `--since` filter and found no
warnings. Existing container timestamps were approximately two seconds behind
that host timestamp. The failure was retained, and the same freshly recreated
container's existing bounded logs were read again. Neither callback was repeated.
No application change was needed for this observation correction.

A real IAM owner profile read succeeded afterward. All 27 selected
business/configuration/audit hashes and four complete inbox/task/outbox/conversation
fingerprints stayed equal. Schema 31, manifest `mvp-30`, original configuration
file hashes and the source freeze matched. The accepted bot remains installed on
the disposable stand. No production service or developer Loom stand changed.

## Evidence and limits

Ignored native proof is under `.cache/shuttle-live-test/trace-logs-qa/`:
`checkpoint.json` and the two safe records in `correlated-logs.json`.
Build/lint evidence and source freeze are under
`.cache/shuttle-work/trace-logs-qa/`; Linux test logs/terminal state are under
`.cache/shuttle-work/container-results/zinusya-trace-logs-qa-20261015.*`.
The native helper and its observation correction received fresh independent
read-only reviews with no actionable findings.

Routing/claim and deferred lease-release diagnostics cannot promise task-attempt
IDs because their existing contexts precede that span. Native task-attempt fault
injection, Grafana derived fields, Cloud ingestion and retention were not tested
here. The earlier full tracing/outage failures and explicit operator recovery
remain [separate and unchanged](shuttle-tracing-qa-2026-10-15.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
