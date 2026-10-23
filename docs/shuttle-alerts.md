# Zinusya operational alerts

The prepared [Prometheus rules](../deploy/shuttle/observability/alerts.yaml) use
existing private metrics. They add no application code, schema, service, database
query or public endpoint. They are not installed in Grafana Cloud by this change.

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

## Local validation

Use the Prometheus promtool version pinned by CI. From the repository root, check the rule syntax and run its synthetic behavior cases:

```sh
promtool check rules deploy/shuttle/observability/alerts.yaml
promtool test rules deploy/shuttle/observability/alerts.test.yaml
```

The [behavior cases](../deploy/shuttle/observability/alerts.test.yaml) cover holds/recovery, empty or missing snapshots, intentional user wait, stale collection, independent labels, uncertainty and due-state thresholds. Synthetic time does not require real-time waiting. Rule validation does not prove notification delivery; that requires a separate configured environment check.
