# Zinusya operations dashboard

Import [dashboard.json](../deploy/shuttle/observability/dashboard.json) into
Grafana and select a Prometheus datasource. It uses built-in Stat and Time series
panels; no plugin, recording rule or installed alert pack is required. The
dashboard introduces no application metric, Go dependency or schema change.

The initial deployment is `instance="complynx.net"`, with the fixed
`job="integrations/zinusya"` from the [Alloy rollout example](shuttle-server-rollout.md).
If other deployments are needed, edit the custom **Deployment instance** variable
to list their exact instance labels. Keep it single-select without an All option.
An expected deployment stays selectable even when its target is absent. Change
the fixed job selectors if the scrape job differs; do not add a duplicate scrape
to make the dashboard work. The datasource variable is used by every panel. Instance selectors use
`:doublequote` with exact equality; regex escaping inside a PromQL string is not
a substitute for literal-value interpolation.

The thirteen panels show metrics scrape status, three independent snapshot
freshness states, task counts/ages, outbox chunk counts/ages, due River jobs and
scheduling lateness, pool connections, returned callback rate/p95, positive
introspection-cache hit ratio and unexpired-credential fallback decision rate.
Time axes use the viewer's browser timezone. These operational timestamps are
not event deadlines or a replacement for event-local time handling.

## Reading the panels

The current status panels use instant queries. Range charts leave gaps instead
of connecting nulls. Unknown, unavailable or stale data is never interpreted as
an empty backlog. Each snapshot count/age query requires `up=1`, successful
collection, a positive last-success timestamp newer than 120 seconds, and snapshot
age at most 120 seconds. These are the same predicates as the existing alert
pack. One failed family does not hide healthy independent families.

The snapshot status returns Fresh only for that complete predicate. It returns
Unavailable for a present target that fails it, including a down scrape. A missing
target returns no data, displayed as Unknown. A datasource query error remains an
error, not a health verdict. The dashboard does not claim that the application is
healthy merely because a metrics scrape succeeds.

Task age includes intentional user waits. Outbox counts are chunks, not users or
messages; task and outbox uncertainty may describe the same incident. Do not add
them to count affected people. Queue age is lateness past `scheduled_at`, including
overdue polls, rather than time without global progress. Zero unresolved work
does not exclude terminal failures. Pool gauges describe local pgx connections,
including the instance-lock connection; they do not establish database readiness.

Worker rates and p95 cover normally returned callbacks including callback-owned
cleanup, not task completion or user response latency. Rates are applied before
aggregation. No histogram observations means undefined p95. Zero cache lookups
means an undefined hit ratio. Neither is replaced with zero. Fallback rates count
successful singleflight renewal decisions using an unexpired credential after a
transient failure, not callers or all IAM failures. No IDs, payloads or event names
are added as dashboard labels.

## Verification and deployment boundary

`python3 tools/test-dashboard.py <temporary-directory>` extracts the actual saved
panel expressions and generates promtool cases. Run the generated
`dashboard.test.yaml` with the same pinned Prometheus image as the alert tests.
CI has a separate dashboard query step. It exercises genuine empty/nonzero
snapshots, failed/missing/stale collection metadata, down/absent targets, target
isolation, cache ratio and callback histogram semantics. NaN is tested by its
inequality to itself, because promtool cannot directly compare NaN samples.

Native Grafana import, all-panel queries and manual visual failure/recovery
acceptance passed; see the [QA report and retained failures](shuttle-dashboard-qa-2026-10-15.md).
This is not a production or Cloud installation. Existing alert activation and notification
choices remain in the separate operator gate.

The disposable QA tool is official Grafana OSS 13.2.1, image
`grafana/grafana@sha256:f772d434e8fab0049deb2b1b30abd43342bcfca1537614aa8d36080232cf4283`.
Grafana's core OSS license is [AGPL-3.0](https://github.com/grafana/grafana/blob/main/LICENSING.md).
It is an external QA service, not linked into the bot. The artifact uses Grafana's
[Classic JSON model](https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/view-dashboard-json-model/)
for import. Prometheus remains the already pinned Apache-2.0 QA tool.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
