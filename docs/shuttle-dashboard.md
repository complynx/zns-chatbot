# Zinusya operations dashboard

Import [dashboard.json](../deploy/shuttle/observability/dashboard.json) into
Grafana and select a Prometheus datasource. It uses built-in Stat and Time series
panels; no plugin, recording rule or installed alert pack is required. The
dashboard introduces no application metric, Go dependency or schema change.

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
