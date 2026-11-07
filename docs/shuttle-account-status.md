# Owner registration and payment status

The source adds a read-only account view to the embedded Mini App and the same
owner read to the agent toolbox.
It creates no registration, pass, payment,
expectation, event choice or Loom run.
The selected architecture is in
[the plan](shuttle-account-status-plan.md).

## API

`GET /app/account` uses the existing `ZinusyaSession` capability and exact Origin
rules. Optional query arguments are `after` (canonical nonzero event UUID) and
`limit` (1–20, default 5). Duplicate or unknown arguments and malformed encoding
return 400. No caller supplies an owner, subject, organization, role or pass ID.

Agent `zinusya_registrations.list` at `/internal/agent/registrations/list` accepts
the same optional `after`/`limit` through the existing protocol-v1 read envelope.
Manifest revision is `mvp-24`. The operation is read-only, without confirmation,
write idempotency keys or external disclosure. A delegated administrator still
receives only that account's own records.

Both return `observed_at`, `events`, `has_more` and optional `next`. Event UUID
keyset order is stable; each page is a fresh snapshot, not one transaction held
across navigation. Item bytes are bounded at 32 KiB per page, in addition to the
count limit. Empty collections are successful reads, not authentication failures.

## Meaning and authorization

Rows originate only from this owner's live registration table within the current
bot/organization/subject binding. Include waitlist and assigned records, fully
paid passes, ended events and closed registration sales. This list is deliberately
different from actionable-event discovery and unpaid payment expectations. Imported
historical memos do not become active registrations or ledger credit here.

Each item exposes safe event name/ID, event IANA zone and local dates, plus the
owner's registration ID/version/role/state/request time. A disabled registrations
module retains that owned snapshot with `registration_enabled=false`, but omits
the entire pass/payment block. Unavailable details must never be shown as zero,
unpaid, cancelled, or permission to perform another module action.

Enabled assigned rows include the frozen pass terms, confirmed balance and pending
evidence count. Confirmed totals come from the existing ledger summarizer; pending
evidence never increases them. Deposit and full-payment flags retain their existing
meaning. Current evidence deadline follows initial/top-up policy and the frozen
deposit snapshot. An unpaid balance past that deadline is overdue; the pass remains
assigned. Fully paid passes are not marked overdue. The UI shows human-confirmed
amounts separately from awaiting evidence.

One repeatable-read transaction locks the current owner binding and uses that
connection for registration/pass, confirmed balance and pending evidence reads.
No transaction crosses an IAM request. Mini App authorization checks the session
and IAM before the projection, then checks the current session again afterward.
Expiry, logout or a changed binding observed by that final check suppresses the
response. No authority queue, reviewer identity, evidence file, partner details,
IAM token or administrator event configuration is exposed.

## Interface

The existing en/ru account page lists registrations with Refresh, First page and
Next page controls. Pages replace the previous records, keeping browser memory
bounded. A failed next-page read retries the same cursor. Authentication rejection
clears saved capability and visible account details. A transient read failure
retains the session and offers retry.

Names are text nodes. Money uses integer decimal digits and the server's currency
scale; it is not rounded through floating-point currency formatting. Dates use
each event's explicit IANA zone, with seconds, numeric UTC offset and the IANA
zone named on screen. Repeated local hours across DST remain distinct. The snapshot
observation time is displayed as an explicit UTC instant. Viewing/pagination does
not persist a business event selection or claim that the agent selected an event.
Future forms/actions need their own durable receipts and reactive agent context.

## Current checks and limits

Architecture and senior Go consultations converged before implementation.
Focused Linux race checks cover waitlists, unpaid/partial/
fully paid state, pending evidence, ended events, disabled modules, owner/admin/
bot/org isolation, one-connection operation, pagination, restart/logout and session
expiry while a DB read is delayed.

The first delayed-read test failed because it searched for handwritten `SELECT *`
text, while sqlc emits explicit columns.
It now observes the actual ungranted
PostgreSQL relation lock.
Original failing logs remain in local evidence.

The served native JS matches that final source byte-for-byte.
No
production readiness is claimed.
