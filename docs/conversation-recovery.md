# Conversation recovery

Shuttle uses Loom's chat inbox when the server advertises
`conversation_recovery.version: 1`. Apply Loom's migrations and stop older Shuttle
workers before enabling this capability. Prepared legacy requests keep their
original endpoint, body and idempotency key until a typed admission rejection
proves that creation was not accepted. A `chat_busy` or
`pending_conversation_messages` rejection can then route that text through the
ordered chat inbox.

The delegated customer principal needs `chat.read`, `runs.control` and
`runs.create`. Check these permissions before enabling recovery. A capability
advertisement does not grant access to a customer's chats or runs.

## Message delivery

Each customer message has a frozen body and a stable key. Shuttle retains blocked
messages in source order. A delivered receipt prevents another inbox delivery.
Observer ownership and predecessor fencing commit in one local transaction.
Onboarding and deferred `/start` requests keep their original child identities
across handoff and restart.

When discovery reports a terminal run, an ordinary new request uses `POST /runs`
and current Shuttle creation settings, including output validation. Inbox
steering and replacement retain the existing run's settings. A concurrent finish
after discovery can still create an inbox new round under Loom's contract.

A typed `no_run` rejection before acceptance permits rediscovery and ordinary
creation. A typed `replacement_target_changed` rejection permits correcting the
target before acceptance. Unknown outcomes preserve the original body and key.
Accepted messages never use these fallbacks.

A typed `not_found` may fail a message locally only when no submission could
have been accepted. The worker saves submission intent before HTTP I/O. A lost
response, interrupted attempt or missing historical proof keeps the message
uncertain; a later 404 cannot discard it or authorize a new paid run.

The existing `/input` path already supplied the full profile prompt for steering.
Inbox messages preserve that context and add reaction and reply guidance. The
complete frozen prompt also supports a run finishing before inbox acceptance.

## Retry limits and blockers

Inbox delivery uses a two-minute retry window. After expiry, another customer
message opens a new window for the saved earlier message. Parked jobs check for
this change once a minute. Already delivered messages continue observation.
Initial discovery and failed run reads also slow to minute checks after two
minutes.

Old messages queued behind earlier work use ten-second order checks instead of
minute waits. This is separate from the minute checks for a known funding,
access or operator blocker.

Known blockers that require funding, model access or operator action use minute
checks immediately. Transient delivery and earlier-message retries use bounded
exponential backoff within the window. Restart preserves the delay; newer input
opens a fresh window. Revoked customer access stops Loom I/O; an administrator
must restore current access before fetching a retained receipt. Local closure
does not accept an operator's assertion as a Loom abandonment receipt.

Funds, authorization, service outages and uncertain effects have separate
statuses. An unreadable run does not prove completion or cancellation. This also
applies to ordinary observed runs: an access-hidden run can return a typed 404.
Shuttle retains ownership and waits for access restoration or authoritative
recovery evidence. It does not automatically abandon messages or create paid
successors without customer input.

A superseded observer stays parked until a local successor attaches. This keeps
its onboarding and deferred requests available for handoff. An administrator can
use confirmed, audited local closure for a parked observer without a pending
inbox message. This fences the local task and releases its conversation pointer;
it does not stop or reconcile the upstream run. Check upstream state first.
This is also the recovery path after confirmed upstream chat deletion, when
there is no retained inbox input. A typed 404 alone is not deletion evidence.
An unresolved inbox receipt still prevents local closure.

## Operator reconciliation

Local closure cannot withdraw a retained Loom message. First confirm delivery or
explicitly abandon the message in Loom under the original actor.

To withdraw a known undelivered message, POST to
`/internal/admin/tasks/{id}/abandon-recovery` with `operation_id`,
`expected_version`, `reason` and `confirm: true`. This is a separate, explicit
operator action. Shuttle rechecks customer authorization, freezes the withdrawal
intent, and calls Loom's abandonment endpoint with the original client and user.
Until its receipt is known, the worker does not replay that input. After a lost
response, retry the exact operation and original expected version. Changing the
operation ID, actor or reason conflicts with the retained intent.

Only Loom's matching abandoned receipt permits the receipt, audit, local fence
and closure to commit together. Unknown responses keep the task and original
message intact. If Loom proves the message was already delivered, withdrawal
returns a conflict and normal receipt replay/observation remains available.
Withdrawal does not stop a run, undo effects, or renew a session. If no message
ID is known, first recover the receipt; an operator cannot invent one.

If Loom cannot read the original chat, it also cannot confirm withdrawal. The
retained input continues to block later messages. Restore the original actor's
access and retry the same operation. For a confirmed upstream deletion, a Loom
operator must restore access or provide an authoritative disposition through
Loom before local closure. Shuttle has no override that guesses abandonment
from a 404, or replaces the missing receipt with an operator assertion.

For an unresolved inbox receipt on a task in `auth_unknown` or `delivery_unknown`,
an administrator can POST to
`/internal/admin/tasks/{id}/reconcile-recovery` with `operation_id`,
`expected_version`, `reason` and `confirm: true`. This explicitly retries the
original frozen request and can deliver work. Current customer authorization is
checked again. The receipt and audit entry commit together.

Tasks parked in `awaiting_service` or `awaiting_authorization` do not use this
endpoint. Their worker retains the normal retry policy.

A delivered receipt queues observer attachment and result delivery. An existing
healthy observer keeps ownership. An abandoned receipt permits a separate
confirmed local closure with the new task version. Reconciliation never renews
a session or abandons input. Retry the same operation after a lost response.
A definitive Loom rejection returns its HTTP status and code; repeating an
unchanged rejected request does not resolve it.

This endpoint does not resolve an unknown Telegram send receipt. It refuses a
definitively rejected inbox message, or a settled inbox receipt whose remaining
uncertainty is Telegram delivery. Use local delivery resolution for those cases.
Shuttle never resends an uncertain Telegram message automatically.

## Unknown authorization

The current observer owns session renewal. A version conflict can adopt only a
durable newer receipt for the exact session. An unknown renewal remains recorded
in the checkpoint and audit. Subject-authorized observation can continue without
guessing a session version.

Loom PR #69 has neither idempotent renewal nor a session-version read endpoint.
After a lost renewal receipt, automatic renewal stays blocked unless another
local task has a newer exact-session receipt. Closure and inbox replay cannot
repair this upstream capability gap. The status asks an operator to reconcile it.

An untyped proxy response cannot prove a mutation was rejected. This includes a
401 or 429 on non-idempotent session renewal: Shuttle records an unknown outcome
rather than retrying a mutation that might already have succeeded.

## Money and input formats

Loom owns shared-wallet accounting, effect reconciliation and successor admission.
A successor receives the current configured per-run allowance. Shuttle never adds
funds, carries a narrower original cap, or replaces repeatedly to evade limits.
Wallet exhaustion requires funding; allowance exhaustion asks the customer
whether to continue.

Version 1 accepts text. File messages already contain durable
`zinusya_files.fetch` references; binary access keeps its authorization checks.
Reply context is presentation metadata and grants no consent. Protocol questions
and approvals keep their original endpoints and consent checks. Native commands
keep their existing handling. Continue/Stop buttons remain ordinary customer text
for the agent to interpret. No cross-chat import is used.
