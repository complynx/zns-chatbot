# Pass recipient batches

## Low-level domain methods

Implemented domain methods: `passbooking.Service.AdminAssignBatch` and
`AdminCancelBatch`. These accept 1–100 distinct recipients in one event. Each
recipient supplies its own stable idempotency key and optimistic actor/target
versions. Assignment items expose all existing single-recipient options. Invalid
fields, duplicate targets/keys and mixed events reject the envelope before writes.

Recipients execute in input order through `AdminAssign` or `Execute`. Each
transaction rechecks current authorization and retains pair updates, prices,
capacity changes, queue allocation, notices and existing history behavior. There
is no additional SQL mutation path, schema migration or batch-wide transaction.
Results follow input order: `succeeded`, `rejected` with a public domain error,
`interrupted`, or `not_attempted`. Domain failures continue; infrastructure errors
or canceled contexts stop execution and return partial results plus an error.
An interrupted item's commit can be uncertain: retry the exact command/key.

Successful item markers survive restarts in the existing operation ledger.
The caller must retain the input list to resume unfinished work. Replaying an
unchanged successful item does not append capacity or send notices twice. Changed
payloads using an already successful key return `idempotency_conflict`. Failed
items have no durable result marker and are evaluated again on retry.

Versions are never silently refreshed. Earlier items may change later target or
actor versions through pair updates or queue allocation; those later items report
`pass_booking_stale`. The adapter must present fresh state for an explicit new
command. Do not count replay outcomes as newly assigned passes; assignment counts
describe the original per-item command.

## Python comparison

Python `handle_passes_assign` and `handle_passes_cancel` in
`zns-chatbot/plugins/passes.py` loop recipients with per-recipient failures.
Assignment options are provided by the existing Go single-recipient service.
Assignment remains global-admin only. Cancellation permits a global administrator
or a payment administrator for that event. Shared domain authorization locks and
rechecks the current membership, including on replay.

Python deletes canceled applications and does its final queue recalculation after
the loop. Go retains canceled booking/history records and performs transactional
partner/queue work per item. This layer preserves those existing Go guarantees.
Python duplicate recipients and arbitrary list lengths are not carried over.

## Runtime adapter

Telegram supports `/passes_assign`, `/passes_cancel`, `/passes_uncouple` and
`/passes_tier`. `--pass_key` selects an event explicitly. When omitted, the
commands select the latest started sale among active events, otherwise the
earliest future sale, using each event's minimum tier start. That resolved default
is persisted with the Telegram update before mutation and cannot drift on retry.
Quoted names/comments are supported without shell evaluation. Assignment accepts
`--price`, `--type`, `--comment`, `--skip`, `--append_to_tier`, `--create_last`, or
`--leader`/`--follower` with `--create_name`. Recipient IDs must be positive and
unique; the limit is 100. Unknown recipients produce individual rejected results.

`POST /v1/passes/batches` accepts an explicit event, actor-scoped key, action,
Telegram recipient list and assignment options. Authentication supplies actor
identity. Clients cannot inject target-owner IDs, optimistic versions, or
per-item keys. The service grounds those once under the event lock and stores
immutable input in `core.pass_admin_batches` (migration 043). It then runs existing
single commands and stores terminal outcomes in input order. Infrastructure
interruptions leave the next item pending. A command and its outcome commit
atomically; uncertain commits replay the same
command/key and do not repeat capacity, payment or notification effects. Terminal
rejection outcomes do not silently retry with new
versions. A changed payload under the same batch key conflicts.

One batch row serializes concurrent runs. Each recipient uses one transaction:
the unchanged domain mutation runs inside a savepoint, and its terminal result
commits with the mutation. A domain rejection rolls back the savepoint before
storing that rejection. No connection is held while waiting for a second pool
connection; concurrent identical requests also work with a one-connection pool.
The normal bot inbox retains failed updates and schedules retries across process
restarts. API-only callers must retry their same request; there is no independent
background worker for abandoned API requests. Bot result messages are at-least-once
if Telegram delivery succeeds before an interruption.
All command guidance and result summaries use English/Russian catalog messages.

`/passes_uncouple EVENT ID` uses the shared global-admin command and preserves
per-user prices. `/passes_tier` allows global or event payment administrators and
returns the allocator snapshot: balance, assigned/paid and waitlist role counts,
current admin assignment tier, configured amounts, prices, dates and explicit
usage. The detailed report includes next configured tiers, date gates, effective
usage and distributed-couple overflow. Assigned-unpaid and waitlist counts use
the balance exclusions; full assigned-plus-paid counts include excluded entries.
A distributed-couple candidate is evaluated only when the filtered waitlist has
both roles. With no such waiting pair, the report has no couple candidate even
when the current tier has room. These rules preserve the historical report.
Natural-language batch proposal binding is not added by this command adapter.

## Runtime verification and acceptance

Focused PostgreSQL tests cover unknown-recipient continuation, downstream stale
versions, immutable replay after service reconstruction, payment-admin cancellation
and revocation, English/Russian Telegram command flows, uncoupling prices and a
synthetic bookkeeping failure, atomic rollback, and concurrent same-key requests
through a one-connection pool. Parser coverage
checks quoted names/comments and ambiguous/unsupported options. These checks do
not replace independent Code QA or Telegram-like Functional Senior QA; acceptance
status belongs in `PROGRESS.md`.

## Domain verification

`go test ./integration -run '^TestPassAdminBatch' -count=1 -parallel=1`
passed against local PostgreSQL on port 55432 (five cases, isolated
databases). Cases cover continuation after a rejection, reciprocal pair assignment,
capacity/replay safety after service reconstruction, stale downstream versions,
cancellation survivor behavior, revoked authorization on replay, envelope
validation before writes, canceled context and infrastructure interruption.

Pinned GolangCI-Lint 2.14.0 `run ./internal/passbooking/... ./integration/...`
reported zero issues. No product stand was rebuilt or changed for this slice.
