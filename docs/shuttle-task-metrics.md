# Unresolved-task snapshot metrics

The private `/metrics` endpoint exports a deployment-wide snapshot from the
application's database schema. It does not change task execution or authorization.

| Prometheus name | Meaning |
| --- | --- |
| `zinusya_tasks_unresolved{state}` | Unresolved tasks in the last valid snapshot |
| `zinusya_tasks_oldest_accepted_age_seconds{state}` | Oldest age since acceptance in that snapshot, advanced between collections |
| `zinusya_tasks_collection_success` | Whether the latest completed snapshot attempt succeeded |
| `zinusya_tasks_last_success_timestamp_seconds` | Application Unix timestamp of the last successful collection; initially zero |
| `zinusya_tasks_snapshot_age_seconds` | Elapsed time since that success; absent until the first success |

The fixed state labels are `accepted`, `creating_run`, `running`, `awaiting_service`,
`awaiting_authorization`, `auth_reauthorizing`, `awaiting_user`, `auth_unknown`,
`delivery_unknown` and `other`. SQL folds unknown states to `other` before grouping,
so result size and label cardinality remain bounded. It excludes `completed`,
`failed`, `cancelled`, `delivery_failed` and `closed` rows. Empty states have zero
count and zero age after a successful query.

Intentional user waits count here. Authorization uncertainty also counts, and
delivery uncertainty counts even though it is terminal for normal task execution.
These values are not runnable River queue depth, time in state, retry delay or
worker utilization. A terminal task can still have unfinished outbox delivery;
zero unresolved tasks does not prove there are no incidents or pending sends.

Age comes from a statement-stable PostgreSQL timestamp minus the oldest accepted
timestamp, using `timestamptz`. It advances by local elapsed time after receipt of
that sample. It is an operational estimate, with the query's bounded sampling lag;
it is not a business deadline. Signed future ages are retained in the cache and
clamped to zero only at observation. Empty-state ages stay zero. Nonfinite or
otherwise invalid samples fail collection rather than emit invalid values.

## Collection and failure

One lazy callback refreshes at most once per 30 seconds after a completed attempt,
including failures. A separate 250 ms context covers pool acquisition and the
aggregate query. It publishes rows, validity and timing metadata as one immutable
generation. No worker, instance-lock loop or lifecycle sampler is added.

The pinned OTel SDK serializes callbacks. Concurrent scrapes can therefore share
the bounded refresh delay; they are not wait-free. The Prometheus exporter does
not inherit HTTP request cancellation, which is why the explicit query timeout
is required. The timeout is a budget, not a real-time scheduling guarantee.
The independent [outbox](shuttle-outbox-metrics.md) and
[River due-job](shuttle-queue-metrics.md) collectors use their own caches and queries.
If all three families are due, collection can incur three 250 ms budgets plus
overhead. Shared private snapshot/gauge mechanics preserve separate family health.

After failure, collection success is zero, the last-success timestamp is retained,
and task count/age series are omitted until a successful refresh. This avoids a
false zero backlog and stale rows presented as a successful new sample. Other
metrics still export and the endpoint can return HTTP 200. Before any success,
the timestamp is zero and no task samples or snapshot-age value are emitted.

Cached success describes the most recently completed attempt, not current database
availability. The freshness metric reveals its age. Never use these cached
operational observations for access, allocation or payment decisions. No IDs,
payloads, profiles, event names, DSNs or error messages become labels or values.
The endpoint stays private; the public listener does not expose it.

## Migration and verification boundary

Migration 26 adds only a partial `(state,created_at)` index with exactly the query's
terminal-state exclusion predicate. Down removes only that index. It rewrites no
data formats or rows; the previous application can still operate during rollback.
The index supports avoiding retained terminal history, but exact aggregation still
scales with the unresolved population and can exceed its budget. Counts are never
silently truncated. See [the implementation plan](shuttle-task-metrics-plan.md).

Actual
Goose Down/Up preserved tasks, changed-payload intake deduplication and an original
event operation receipt.
