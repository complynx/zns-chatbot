# Task-service pool startup guard

Task-service construction now rejects a missing database pool or
`pool_max_conns=1` with `task database pool requires at least two connections`.
Validation runs before queue construction or any instance lease is acquired.
The startup DSN remains the existing configuration mechanism; no setting,
dependency or migration was added.

The instance advisory lease retains one pooled connection for the lifetime of
the application.
Startup then needs another connection for deployment binding,
recovery and worker queries.
This was observed during the prelink endpoint fixture
QA; its failed/stopped run remains in the original evidence.

Two is the minimum application pool size, not a recommended production sizing
value or a limit on total PostgreSQL sessions. River's listener acquires and then
hijacks a connection outside pool ownership. Normal pool defaults remain; worker
concurrency and server capacity still determine operational sizing. Database-only
stores and API-only operations can retain one-slot pools: the guard belongs to
task.Service, not db.Open.
