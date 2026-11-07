# Private outbox metrics

The internal `/metrics` endpoint exports deployment-wide unfinished delivery
counts. Public `/metrics` remains unavailable. These gauges count outbox chunks,
not tasks, users or complete logical messages. They do not retry delivery or
change application state.
Separate payment/pair notification tables and River runnable jobs are not counted
by this outbox query.

| Metric | Meaning |
| --- | --- |
| `zinusya_outbox_unresolved{state}` | Number of included outbox rows |
| `zinusya_outbox_oldest_created_age_seconds{state}` | Age since creation of the oldest included row |
| `zinusya_outbox_collection_success` | Whether the latest completed snapshot attempt succeeded |
| `zinusya_outbox_last_success_timestamp_seconds` | Last successful collection's Unix timestamp; zero before first success |
| `zinusya_outbox_snapshot_age_seconds` | Elapsed time since successful collection; absent before first success |

Only `state` is attached to count/age points. Fixed values are `pending`,
`retryable`, `send_inflight`, `delivery_unknown` and `other`. SQL folds unknown
persisted states to `other` before grouping. Delivered, blocked and permanently
failed rows are excluded. Payload, receipt, dedup key, raw unknown state, identity,
chat, task and event values are never labels.

These metrics are independent of task state. A closed or completed task can own
included chunks. `delivery_unknown` records external uncertainty and remains in
the outbox after local task closure. Pending suffix chunks can also wait behind
an earlier uncertain receipt. Neither bucket implies work can safely be sent.
Permanent failures use existing delivery telemetry and operator diagnostics;
zero unfinished count does not mean absence of delivery incidents.

Age is measured from `shuttle_outbox.created_at`, including retries and ordering
dependencies. It is not runnable latency, time in current state or age since task
acceptance. Signed future ages are preserved in the cache; observation adds cache
elapsed time before clamping to zero. Empty states have zero count and age.
Database/application clock differences and query-to-publication delay make this
an operational estimate, unsuitable for deadline enforcement.

## Collection and failure

A lazy immutable snapshot is refreshed at most once per 30 seconds after the
previous attempt finishes, including failed attempts. One 250 ms child context
covers connection acquisition and the SQL aggregate. No query is made on cached
scrapes; there is no background collector or task-loop hook.

The task and outbox families share private scalar snapshot/gauge mechanics but
have separate queries, caches, callbacks, validity and freshness. A failed outbox
refresh omits its counts/ages and exports failure; it preserves the last successful
timestamp and increasing snapshot age. It does not return healthy zero counts,
keep old successful points or invalidate the task family. Before first success,
the successful timestamp is zero and no snapshot-age point exists. The callback
returns no error so other metrics can still be exported.

The pinned OTel SDK serializes callback collection. If task, outbox and
[River due-job](shuttle-queue-metrics.md) caches
are due, a scrape can incur three 250 ms database budgets plus cancellation and SDK
overhead. Concurrent HTTP requests can queue behind collection. This is not a
250 ms endpoint guarantee or a wait-free scrape design. The pool gauge callback
itself still performs no database query or connection acquisition.

Migration 27 adds a partial `(state,created_at)` index excluding exactly the three
terminal states above. It avoids scanning retained terminal history but does not
bound unfinished population size; timeout means failed collection, never a
truncated count. Ordinary transactional creation can block writes while building.
Down removes only the index and changes no rows or data format.
