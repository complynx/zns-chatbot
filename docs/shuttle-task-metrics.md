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

Focused race checks passed for signed age, bounded grouping, cache isolation,
failure throttling/recovery, removal of previous OTel points, concurrent timeouts,
and real PostgreSQL pool-acquisition timeout with independent pool gauges. Actual
Goose Down/Up preserved tasks, changed-payload intake deduplication and an original
event operation receipt. The initial lint failure is retained separately: a global
state array became local, and the sequential rollback/reapply test no longer uses
parallel subtests or a context-losing helper. Strict lint now reports zero issues.
Fresh correctness, security/quality and final source reviews found no actionable
defects. The full replacement Linux race gate passed all 179 enumerated integration
tests in 12 bounded groups, plus explicit cmd/internal/migrations packages. All 14
runner states ended with exit code zero and no OOM. Temporary QA runners and the
separate QA PostgreSQL container/anonymous volume were removed.

## Owned native acceptance — 2026-10-15

Image `sha256:f67996d1eb687c8c4f2a1d07666135f981801dd5491323e2e385b716ae0e11ac`
is running on the owned `shuttle-live-test` stand. Schema is 26; the unchanged
agent API manifest remains `mvp-26`. A pre-migration custom PostgreSQL backup was
verified by `pg_restore -l` and its `PGDMP` header: 2,175,847 bytes, SHA-256
`5c6e7832d0a9508e5dbb6712f998139cfd1944245f3803879c1a78f3958bf657`.

Six SQL-only task fixtures and a separate conversation exercised user/auth/delivery
waits, two unknown states folded to one `other` series, a future acceptance time,
and zero ages for empty states. These were synthetic operational rows without
inbox updates, River jobs or outbox sends. The private endpoint exposed the expected
counts and ages after a real bot restart; the public `/metrics` returned HTTP 404.
No payload/state canary appeared in the output.

A controlled transaction locked task reads. The collection query timed out,
success became zero, previous task series disappeared, last-success time stayed
unchanged, and pool gauges remained available. After unlocking, failure cooldown
persisted; a later refresh recovered the expected nonzero counts. Removing only
the exact fixture rows restored baseline hashes and fresh all-state zeros.

Actual native Goose DownTo25 removed the index. The previous pool-metric image
then started and completed an owner `account/me` read with real local IAM. The new
image reapplied migration 26 and returned a successful empty snapshot. Row counts
and hashes of 22 selected tables, including tasks, conversations, business records
and six operation-receipt tables, matched the baseline throughout final cleanup
and rollback/reapply. No new Telegram messages or Loom inference were involved.

The retained ignored checkpoint is
`.cache/shuttle-live-test/native-task-metrics-checkpoint.json`; the verified backup
is `.cache/shuttle-live-test/backups/before-task-metrics-20261015.dump`.

This does not add outbox/notification backlog, River runnable latency, agent API,
token-cache statistics, dashboards or alerts. Production acceptance is not claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
