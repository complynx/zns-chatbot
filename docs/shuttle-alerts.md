# Zinusya operational alerts

The prepared [Prometheus rules](../deploy/shuttle/observability/alerts.yaml) use
existing private metrics. They add no application code, schema, service, database
query or public endpoint. They are not installed in Grafana Cloud by this change.

The supported target is one deployment-wide scrape with
`job="integrations/zinusya", instance="complynx.net"`, as in the
[server rollout](shuttle-server-rollout.md). Change both exact labels in the rules,
tests and Alloy configuration together for another deployment. Do not scrape the
same deployment through both old and new job names or sum duplicate snapshots.
The internal River kind `shuttle_task` remains unchanged for durable compatibility.

## Conditions and response

All thresholds are initial operational defaults, not business deadlines or SLOs.
The rule group evaluates every 30 seconds. A condition must remain true throughout
its `for` interval before firing; recovery clears it on evaluation.

| Alert | Condition and hold | Operator action |
| --- | --- | --- |
| `ZinusyaScrapeUnavailable` | Expected `up` is zero or absent for 2 minutes | Check Alloy, private network and bot. Monitoring failure alone does not prove bot failure. |
| `ZinusyaTasksSnapshotUnavailable` | Live target lacks a complete healthy task snapshot for 2 minutes | Check collection health, freshness and PostgreSQL acquisition/query failures. |
| `ZinusyaOutboxSnapshotUnavailable` | Same check for outbox | Treat missing counts as unknown, not zero. |
| `ZinusyaQueueSnapshotUnavailable` | Same check for due River jobs | Check snapshot health before interpreting backlog. |
| `ZinusyaTaskUncertain` | Positive `auth_unknown` or `delivery_unknown` for 2 minutes | Inspect original task/session/operation receipt; do not replay uncertain actions. |
| `ZinusyaDeliveryUncertain` | Positive outbox `delivery_unknown` for 2 minutes | Inspect the stored receipt and actual Telegram chat before considering a resend. |
| `ZinusyaQueueOverdue` | Positive due count and scheduling lateness over 300 seconds, continuously for 5 minutes | Check available workers, dependencies, scheduler promotion and the existing leader-lease diagnostic. |

A healthy snapshot requires `collection_success=1`, positive last-success
timestamp newer than 120 seconds, and snapshot age at most 120 seconds. This
allows the existing 30-second cache and 15-second scrape interval. Absolute
last-success time also detects a frozen previously successful timestamp, even
if its exported age stops changing. Failed or absent initial collection is not
an empty queue. Snapshot warnings are suppressed while the target scrape is
unavailable; uncertainty/backlog alerts require a healthy corresponding snapshot
and live scrape. Failure of one family does not invalidate the other families.

Task and outbox uncertainty can describe the same incident; their values are not
distinct user counts. `awaiting_user`, total task age and long active inference
do not trigger a task-stalled alert. Queue age measures lateness against
`scheduled_at`, including overdue polls; it does not prove global lack of progress.
Future snoozes are excluded by the existing due-job reader. No alert resends a
message, reauthorizes a session, resumes a run or changes a payment.

This pack does not yet detect terminal `delivery_failed`, provide end-to-end
business latency, or prove the five-second hard shutdown ceiling. A process
killed before the next scrape cannot report a final histogram reliably; shutdown
deadline acceptance and log/container-based detection remain separate work.

## Local and CI validation

The test-only tool is official Prometheus/promtool 3.15.0, pinned to the same
image digest used by the Loom observability validation:

```sh
docker run --rm --network none --read-only --memory 128m --memory-swap 128m \
  --cpus 1 --pids-limit 64 --cap-drop ALL --security-opt no-new-privileges \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m -e GOMEMLIMIT=80MiB \
  --mount "type=bind,source=$PWD/deploy/shuttle/observability,target=/rules,readonly" \
  --workdir /rules --entrypoint /bin/sh \
  prom/prometheus@sha256:efd719c99d83b060d9daefdcf00360461adf279f45ef5391f8d111892118753e \
  -ec '/bin/promtool check rules alerts.yaml; /bin/promtool test rules alerts.test.yaml'
```

Run from the repository root on Linux; PowerShell uses a resolved absolute mount
path instead of `$PWD/...` and its own line continuation. The image is downloaded
before the network-disabled test container starts. It is not part of the bot
image or Go dependency graph. Prometheus is Apache-2.0 licensed; see its
[versioned license](https://github.com/prometheus/prometheus/blob/v3.15.0/LICENSE).

The [behavior cases](../deploy/shuttle/observability/alerts.test.yaml) exercise
holds and recovery, empty snapshots, intentional user wait, missing target,
missing/failed/never-successful/stale collection, independent target labels,
uncertainty, all three due states and threshold/count boundaries. Promtool uses
synthetic time; it does not wait minutes in real time. The CI workflow runs the
same commands. No new full Go-suite pass or cloud notification delivery is implied.

## Server activation gate

Keep the existing Alloy Docker logs pipeline and `metrics_service` remote write.
After authorized pilot deployment, verify the single private scrape and exact
job/instance labels in Grafana Cloud. The old rollout job spelling was only an
example; if an operator has deployed it, coordinate the rename and avoid running
both targets concurrently. Confirm the metric names and freshness before enabling
rules.

Upload the rules to the selected Grafana Cloud Prometheus/Mimir ruler namespace
using the stack's existing operator tooling; the rule file alone is not a running
alert service. Grafana documents [mimirtool rule provisioning](https://grafana.com/docs/grafana-cloud/observe-and-act/alert-and-measure-reliability/alerting/set-up/provision-alerting-resources/use-mimirtool/).
Review existing rules and namespace ownership first. This change supplies no
Cloud credentials and does not install another Prometheus on the server.

Configure a deliberate notification route/contact point and verify a synthetic
firing and resolved notification with the operator before claiming alert delivery.
No external message or Cloud rule is created during local acceptance. Record
the namespace, installed rule revision, chosen thresholds and contact policy in
deployment evidence. Rollback removes only this named `zinusya` rule group from
its chosen namespace; it does not alter unrelated Loom rules or the log pipeline.

Prometheus [alerting semantics](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/)
and [rule-test format](https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/)
are the source contracts for hold intervals and tests.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
