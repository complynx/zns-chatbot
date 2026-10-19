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

## Source verification

Behavioral checks cover nine fixed pairs, cache/failure cooldown and recovery,
private labels, due/future/state/kind/queue isolation in a nonpublic River schema,
and blocked River reads with independent task/pool health. A single real River
client exercises promotion, occupied worker capacity, backlog drain and future
`JobSnooze` exclusion while preserving job history. The worker scenario passed
three consecutive repetitions.

Fresh correctness and code-quality reviews found no actionable defects. Strict
lint reported zero issues. The full Linux race gate passed all 184 enumerated
integration scenarios in 13 bounded groups and the explicit cmd/internal/migrations
packages. All 15 runner states exited zero without OOM. Temporary QA runners and
the separate QA PostgreSQL container/volume were removed.

An earlier two-client fixture exposed a separate startup/stop leadership lease
delay. Changing the metric fixture to start one client does not resolve that
runtime finding. See [the retained observation](shuttle-river-startup-lease.md).

## Owned native acceptance — 2026-10-15

Candidate image
`sha256:d2a10ad3189dd66ecb1c6a09ec373b77020c98f75a4eaf3e50dd54360e0c2ca8`
passed on the owned `shuttle-live-test` stand. Schema remains 27 and the agent API
manifest remains `mvp-26`. Mini App origin and the original settings document
(version 1, identity TTL 0) were preserved.

Fourteen synthetic jobs exercised nine due pairs, three future jobs, a foreign
kind and a foreign queue. All rows were held with transaction locks while the real
bot served metrics. Every due pair counted one; oldest lateness was at least 119
seconds although job creation was two days earlier. Task/outbox health remained
successful. After releasing all fixture locks, the real scheduler and workers
completed all nine due no-op jobs at attempt 1. A fresh snapshot after cache expiry
reported nine zero counts. Closed-task and nonexistent-notification guards kept
these jobs away from IAM, Loom and Telegram delivery.

Cleanup stopped the bot before deleting exactly 14 stamped jobs and their separate
task/conversation, then restarted the candidate. Hashes of 22 selected tables
matched baseline, including original business records and operation receipts.
The fresh empty snapshot had zero counts/ages; public `/metrics` returned 404
during rollout. No Telegram messages or Loom inference were needed. A real
PostgreSQL integration check covers blocked-query failure and independent
task/pool health; controlled-reader collector tests cover cooldown and recovery.
This successful native probe does not establish native query timeout/recovery.

Two failed verifier attempts remain intact. First, multiline `json_agg` output
broke a single-line parser; read-only reproduction justified `jsonb_agg`. Second,
releasing only one scheduled row left other rows in River's scheduling batch
locked: pinned `JobSchedule` uses `FOR UPDATE` without `SKIP LOCKED`. Both attempts
removed all 14 jobs, restored selected hashes and restarted the bot. The accepted
probe released all locks before observing nine completions. These are harness
corrections; no product worker/scheduler code changed and no failed result was
rewritten as successful.

The tiny restored stand's read-only `EXPLAIN (ANALYZE,BUFFERS)` took 0.103 ms for
execution. This is a small-fixture observation, not a production capacity result
or proof that an index is unnecessary at every scale. No speculative index was
added. Only the nine owned stand containers remain; disposable runners and the
separate QA database/volume were removed.

Ignored evidence lives under `.cache/shuttle-live-test/`:
`native-queue-metrics-checkpoint.json` (first failure),
`native-queue-metrics-recovery-checkpoint.json` (second failure),
`native-queue-metrics-unlocked-checkpoint.json` (accepted probe, retaining the
preceding checkpoint's SHA-256), and `queue-metrics-native-plan.json`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
