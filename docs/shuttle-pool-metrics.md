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

## Verification boundary

Behavior tests exercise independent provider scopes and credential exclusion with
an unreachable database address. A real PostgreSQL fixture holds the only allowed
connection while eight concurrent scrapes complete, verifies the acquired/idle/
total/maximum values, and proves the scrapes do not increase pool acquisitions.
Releasing the connection changes the exposed acquired/idle gauges. Focused race
checks passed, both independent source reviews reported no actionable findings,
and strict lint passed with zero issues.

The full replacement Linux race gate passed all 178 enumerated integration tests
in 12 bounded groups, plus the explicit cmd/internal/migrations packages. All 14
runner states ended with exit code zero and no OOM. The temporary QA runners and
separate QA PostgreSQL container/anonymous volume were removed.

Owned native rollout and restart acceptance passed on 2026-10-15, using image
`sha256:e30461c8436fd5db70b6e77ebf10aadd19b23505f6a156777bdaa10a664ee9a2`.
Schema stayed at 25. Existing row counts and eleven selected business-table
hashes survived rollout, concurrent scrapes and a real bot restart. The private
listener returned all four unlabeled gauges during eight concurrent scrapes;
the public `/metrics` returned HTTP 404 without their content. The before/after
restart samples were acquired 1, idle 2, total 3, maximum 32. These are recorded
samples, not fixed capacity defaults or permanently expected idle counts.
The checkpoint is retained in ignored
`.cache/shuttle-live-test/native-pool-metrics-checkpoint.json`.

No new Telegram messages or Loom inference were needed for this HTTP/real-pool
acceptance. No production acceptance is claimed. Unresolved-task snapshots, accepted-task age,
River runnable latency, outbox backlog, token-cache statistics, dashboards and
alerts remain separate observability work. A task's age since acceptance includes
intentional user/service waits and must not be called time waiting for a worker.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
