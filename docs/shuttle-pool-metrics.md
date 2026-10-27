# PostgreSQL pool metrics

The application's existing private `/metrics` endpoint exports four scoped OTel
gauges through the Prometheus exporter:

| Prometheus name | Meaning |
| --- | --- |
| `zinusya_db_pool_acquired` | Connections currently acquired by application, queue and instance-lock work |
| `zinusya_db_pool_idle` | Idle connections in this application's pool |
| `zinusya_db_pool_total` | Total pool connections, including connections being constructed |
| `zinusya_db_pool_max` | Configured maximum size of this application's pool |

Collecting these four gauges reads one local `pgxpool.Stat()` snapshot. This pool
callback performs no SQL, network request or pool acquisition. Other collectors
on the same endpoint can delay the response: the separate
[task snapshot](shuttle-task-metrics.md) and [outbox snapshot](shuttle-outbox-metrics.md)
each have a bounded database-refresh budget. If both are due, serial collection
can incur two 250 ms budgets plus cancellation and SDK overhead.
No background sampler, new dependency, setting or database
migration is needed. The callback belongs to the existing scoped meter provider
and ends with that provider's shutdown.

The gauges have no labels. They expose no DSN, database name, address, user,
identity, event, task or message content. The endpoint remains private and must
stay restricted by deployment networking. The public listener does not expose it.

Acquired connections include the permanently held instance advisory-lock
connection and River's connections. They are not a count of executing user tasks.
Total may exceed acquired plus idle while a connection is being constructed.
These local statistics do not ping PostgreSQL or prove that idle connections are
healthy. A successful metrics scrape is not a database-readiness assertion.
