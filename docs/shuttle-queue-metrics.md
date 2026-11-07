# Private River due-job metrics

The internal `/metrics` endpoint reports scheduling backlog for the default River
queue in the bot's configured database schema. The public listener does not expose
these metrics. Collection reads no job arguments, metadata or business identifiers.

| Prometheus name | Meaning |
| --- | --- |
| `zinusya_queue_due{kind,state}` | Jobs whose `scheduled_at` has arrived |
| `zinusya_queue_oldest_scheduled_age_seconds{kind,state}` | Oldest lateness against that scheduling deadline |
| `zinusya_queue_collection_success` | Whether the latest completed snapshot attempt succeeded |
| `zinusya_queue_last_success_timestamp_seconds` | Last successful collection's Unix timestamp; initially zero |
| `zinusya_queue_snapshot_age_seconds` | Elapsed time since success; absent before the first success |

The fixed kinds are `shuttle_task`, `zinusya_payment_notification` and
`zinusya_pair_notification`. The fixed states are `available`, `scheduled` and
`retryable`: nine pairs, including zeros after a successful query. The legacy
`shuttle_task` name remains a durable job identifier. Unknown or duplicate result
pairs fail collection rather than create labels. Foreign queues/kinds, future
jobs, running jobs and terminal history are excluded.

Pinned River v0.49.0 fetches due `available` jobs. Due `scheduled` and `retryable`
jobs still need scheduler promotion. These gauges therefore describe due backlog,
not guaranteed fetch eligibility, worker utilization or execution latency. Paused
queues and row locks do not hide due jobs. An intentional user wait can schedule
another poll: a future snooze is excluded; an overdue poll contributes only its
current scheduling lateness, not the whole user's waiting time.

Age uses PostgreSQL's statement-stable timestamp minus the oldest `scheduled_at`.
Local elapsed time advances a cached successful sample. Empty pairs remain zero;
nonfinite or otherwise invalid samples fail collection. This is operational timing,
not an event deadline or evidence for a business decision.

## Collection and failure

The collector shares private snapshot mechanics with task/outbox metrics, with
its own cache and health. It refreshes lazily at most once per 30 seconds after a
completed attempt, including failures. A separate 250 ms context covers pool
acquisition and the aggregate query. Snapshot rows and timing publish atomically.
Existing task/outbox metric names and state-only labels remain unchanged.

Failure exports success zero, retains the last-success timestamp and freshness,
and omits queue count/age samples until a successful refresh. Before first success,
last-success is zero and snapshot age is absent. HTTP 200 can still carry failed
collection health and other metric families. A cached success describes the latest
completed attempt, not current database availability.

The pinned OTel SDK serializes callbacks. If task, outbox and queue snapshots are
all due, collection can incur three 250 ms database budgets plus cancellation and
SDK overhead. Concurrent scrapes can wait behind collection. This is not a 250 ms
endpoint guarantee. Pool and identity metrics perform no scrape-time database/IAM
requests.

River owns its table and indexes. This slice adds no migration, index, dependency
or configuration. Exact aggregation still scales with the due population; a
timeout fails collection rather than truncates counts. No production-scale query
capacity is claimed.
