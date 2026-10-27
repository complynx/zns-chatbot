# Step 0: messaging foundation and operator recovery

Step 0 adds CI and a private operator API for uncertain tasks. It does not add
business modules or complete the production rollout gates in the server guide.

## CI

`.github/workflows/shuttle-ci.yml` checks Go 1.27.2 on Linux with PostgreSQL 17:
reviewed dependency pins, behavioral tests with the race detector, sqlc output,
golangci-lint, the release binary and the Docker image.
It does not publish images.
Later commits require their own green checks.

## Operator access

Use the private listener. Every request requires a verified user bearer token,
the `zinusya.admin` role in the configured API project and a locally active
account in the configured organization. Target tasks must belong to this bot
and to an unbound user or the configured organization. Foreign tasks return 404.
No public Telegram endpoint exposes these operations. The current agent manifest
also exposes role-gated administrative reads and writes with exact-parameter
approval; see [agent administration](shuttle-agent-administration.md) for the
available methods and their individual authorization contracts.

Run the explicit application migrations before starting this version. Migration
00002 creates `shuttle_task_resolutions`; River migrations remain separately
owned by River. Retain this audit table during rollback: dropping it loses the
operator decisions. Do not resume an older binary against closed tasks without
a reconciliation plan, since older code does not recognize the closed state.

## Inspect tasks

`GET /internal/admin/tasks?state=delivery_unknown&limit=50` lists task metadata.
`state` is optional; `limit` is 1..100. Follow the optional `next` UUID with
`after=<uuid>`, retaining the same filter. UUID order is stable, not chronological;
this is a live listing rather than a snapshot of concurrently created tasks.

`GET /internal/admin/tasks/{id}` returns task version and lease, delivery states,
read-only River job metadata and operator resolutions. Message bodies, snapshots,
tokens, session IDs and native queue errors/arguments are excluded. Queue jobs
may disappear after River retention; application task/audit history remains.

## Close an uncertain task

The same local effect is available to an authorized agent through
[`zinusya_administration.close_task`](shuttle-task-closures.md), with persisted
preparation, exact approval and status/reconcile. The operator API below remains
available with its existing receipt contract.

First inspect the task and check the external result using the relevant service.
The API supports local closure only: it neither confirms Telegram delivery nor
cancels a remote Loom run. It does not retry an unknown operation. If a remote run
is still active, handle it separately before treating the incident as resolved.

Send `POST /internal/admin/tasks/{id}/close` with JSON:

```json
{
  "operation_id": "incident-2026-10-12-1",
  "expected_version": 7,
  "reason": "Stop local processing after investigation of a lost receipt",
  "confirm": true
}
```

Closure is permitted for unknown authorization, a pending reauthorization
checkpoint, or an unknown/in-flight delivery. A live task lease or stale version
returns 409. A task with a known outcome also returns 409. Missing confirmation
or a blank reason returns 400. Invalid IDs, bounds or operation keys return 422.

The transaction changes the task to `closed`, increments its version and fence,
blocks pending/retryable deliveries and records the actor, reason, operation key,
previous state and resulting version. It clears the active conversation binding
only if that binding still points to this task. It preserves unknown delivery
records, requests, checkpoints and external run IDs for investigation.

Retry a lost HTTP response with the same task ID, operation ID, expected version
and reason, using the same operator identity. This returns the original audit
result without applying closure again. Reusing the key for a different decision
returns 409. Closed tasks remain closed across duplicate Telegram updates and
worker restarts; they do not resend their outbox.
