# Operational alert validation — 2026-10-15

Seven [prepared rules](../deploy/shuttle/observability/alerts.yaml) passed local
validation. This is acceptance of rule semantics and the current metric contract,
not installation in Grafana Cloud or proof of notification delivery.

Architecture and Go consultations preceded implementation. The result uses
existing metrics, three independent snapshot-health checks and one expected
job/instance. It leaves application code, schema, runtime configuration and
business state unchanged. The [runbook](shuttle-alerts.md) explains thresholds,
operator actions and the activation gate.

## Behavior checks

The official pinned Prometheus image reported promtool 3.15.0, revision
`5241a27fe3c6983549fccc32f6e65917408c63cd`. `check rules` accepted seven rules;
`test rules` passed all twelve named scenarios:

- Healthy empty snapshots and an old intentional `awaiting_user` task stay quiet.
- A missing expected target alerts even when another instance is healthy.
- A failed scrape alerts after its hold, suppresses stale business alerts and resolves.
- A missing task family cannot borrow another instance's healthy data.
- Never-successful outbox collection alerts, then resolves after recovery.
- A frozen successful queue timestamp alerts even with a frozen zero age.
- A missing age series is an incomplete snapshot, not healthy empty data.
- Both task uncertainty states respect the hold and resolve after receipt recovery.
- Outbox uncertainty respects the hold and resolves.
- Overdue available, scheduled and retryable jobs alert and clear after draining.
- Zero due count or lateness exactly at the threshold does not alert.
- A failed task collector suppresses retained uncertainty samples.

Cases evaluate observable alert labels, annotations, firing holds and clearing
with synthetic time. They do not test internal implementation details of the bot.
The same two promtool commands were added to the CI workflow. GitHub CI was not
run or claimed green: this local change has not been pushed.

## Actual metric-contract readback

A read-only scrape from the owned `shuttle-live-test` bot supplied 57 actual
task/outbox/queue series. Only aggregate snapshot families were retained. The
local evaluator added the expected scrape job/instance labels and one successful
`up` sample, anchored its test clock to capture time, and observed no pending or
firing alerts. This confirms metric names/labels and a healthy current snapshot;
the `up` sample represents the successful capture, not Grafana Cloud connectivity.
No live queue or database fault was injected for this check. Failure/hold/recovery
semantics are covered by the twelve synthetic cases above.

The bot stayed on image
`sha256:295cb68a4e3cce29179979ea305552535d98b80873ff9ff1ee802ca6fe2a9470`,
schema 31. All 323 frozen application source/tooling paths and 27 selected
business/configuration/audit hashes remained unchanged. No Telegram message,
Loom inference, new identity, database mutation or service restart was needed.

Both test containers had no network, read-only mounts/root, 128 MiB memory with
no swap, one CPU and dropped capabilities. Both exited 0 without OOM. Their exact
IDs and terminal state were saved before removal; no foreign container was touched.
The test image remains a reusable cached developer tool, not a running service.

## Review and remaining gates

Fresh independent correctness and adversarial quality reviews found no actionable
findings. They assessed PromQL matching, timing, absent-series handling, real metric
semantics, behavioral coverage, constrained CI and the runbook. No Go source
changed, so no new full Go/race-suite pass is claimed. The staging area stayed empty.

Ignored evidence is under `.cache/shuttle-work/alerts-qa/`: `rules-first.log`,
`native-first.log`, `native-readback.json`, `native-snapshot.prom`,
`native-snapshot.test.yaml` and `terminal-cleanup.json`.

Cloud rule installation, notification contact policy and an actual firing/resolved
delivery remain pilot gates. Terminal delivery failures, hard shutdown detection,
end-to-end latency and full causal tracing remain separate work. The previous
OTLP outage/restart failures and operator recovery are unchanged; see
[their acceptance boundary](shuttle-tracing-qa-2026-10-15.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
