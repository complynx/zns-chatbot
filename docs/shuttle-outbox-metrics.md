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
Down removes only the index and changes no rows or data format. The API manifest
remains `mvp-26`.

## Acceptance checkpoint

Source checks passed before and after extracting shared mechanics: behavioral
scrapes cover immutable generations, future/empty ages, failed-attempt cooldown,
recovery, privacy and concurrent collection with independent task health. Real
PostgreSQL checks cover state folding, multiple chunks under a closed task,
terminal exclusions, future timestamps, index rollback/reapply and deduplication.
Integration checks use an actually exhausted pool and an exclusive outbox-table
lock; both retain independent task/pool metrics. Fresh correctness and quality
reviews reported no actionable findings. Final lint passed with zero issues.

The full Linux race gate passed all 181 enumerated integration scenarios in 13
bounded groups and all other explicit Go packages. All 15 runner states exited
zero without OOM. Earlier duplication and redundant-slice lint failures remain
retained with their subsequent passes. No product source changed during the final
gate/build/native acceptance; a 221-file source freeze is retained.

Owned native acceptance passed with application image
`sha256:6dc4c36c28b53db863999b84aebb5cd407232c6a9db5516c8bad9053605711e6`.
A verified PostgreSQL-format backup preceded migration 26 to 27. Ten SQL-only
outbox rows under one closed task exercised chunk counts, terminal exclusions,
unknown-state folding and a future timestamp. There were no inbox updates, River
jobs or Telegram sends for these fixtures. A real bot restart recovered the one
synthetic `send_inflight` receipt to `delivery_unknown`; collection reported the
expected changed counts. Public `/metrics` returned 404.

An actual exclusive outbox-table lock caused the bounded query to fail. Outbox
count/age points disappeared, its last-success timestamp remained unchanged and
task/pool metrics remained available. Unlocking did not bypass failure cooldown;
the next eligible refresh recovered the nonzero fixture counts. Exact fixture
cleanup restored all 22 selected baseline table hashes/counts; a fresh successful
snapshot then reported zero counts and ages.

Actual Goose DownTo26 removed just the new index. The previous application ran
on schema 26, served private pool gauges and passed an owner read through real
Zitadel. Reapplying 27 and restarting the new image preserved the same selected
rows and receipts. Mini App origin configuration survived all switches. Native
QA review caught its missing override before execution; that harness defect was
fixed and a fresh review found no substantive findings.

Evidence is retained under ignored `.cache/shuttle-live-test` in
`native-outbox-metrics-checkpoint.json`, its backup and immutable candidate
override; source freeze, build/lint/race logs and runner states are under
`.cache/shuttle-work`. Temporary QA/build runners and the separate QA PostgreSQL
container/anonymous volume were removed. No production readiness is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
