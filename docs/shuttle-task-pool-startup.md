# Task-service pool startup guard

Task-service construction now rejects a missing database pool or
`pool_max_conns=1` with `task database pool requires at least two connections`.
Validation runs before queue construction or any instance lease is acquired.
The startup DSN remains the existing configuration mechanism; no setting,
dependency or migration was added.

The instance advisory lease retains one pooled connection for the lifetime of
the application. Startup then needs another connection for deployment binding,
recovery and worker queries. Previously a one-slot configuration passed
construction and waited indefinitely at this second acquisition when started
with an unbounded context. This was observed during the prelink endpoint fixture
QA; its failed/stopped run remains in the original evidence.

Two is the minimum application pool size, not a recommended production sizing
value or a limit on total PostgreSQL sessions. River's listener acquires and then
hijacks a connection outside pool ownership. Normal pool defaults remain; worker
concurrency and server capacity still determine operational sizing. Database-only
stores and API-only operations can retain one-slot pools: the guard belongs to
task.Service, not db.Open.

## Verification

A new real-PostgreSQL behavioral test failed against the unchanged constructor:
an error was expected, but nil was returned. The red log is retained. After the
fix, it proves prompt constructor rejection, continued one-slot DB use, successful
startup and a complete inbox/IAM/Loom/Telegram task through HTTP peers with a
two-slot pool. It then stops the service and starts a replacement using a fresh
pool while the old pool stays open, checking instance-lease release independently
of connection reuse.

The first review found no defect but suggested strengthening restart proof with a
fresh pool. That suggestion was adopted; a fresh independent review found no
actionable issue. Strict task/integration lint reports zero issues. Final Linux
race checks passed the complete internal/task package (8 roots, 30 root/subtest
pass events) and three selected real-PG integration tests, without failures/skips.
The neighboring tests preserve endpoint-only one-connection IAM waiting and pool
metrics collection while its only connection is held.

Ignored evidence: `.cache/shuttle-work/startup-pool-qa/`, with red/green/final logs,
terminal state and source hashes. The final 4 GiB runner exited 0, OOM false;
cgroup max/oom/oom_kill counters were zero. Exact owned Go/PostgreSQL containers
were removed afterward; no host ports, test volumes or networks were created.
This is source-level and real-PG behavioral acceptance, not a new main-stand image
or production cutover. The original one-slot task startup hang is fixed; external
IAM/Loom availability, shutdown limits and full provider login keep their separate
acceptance boundaries.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
