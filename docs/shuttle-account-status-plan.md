# Owner account status plan

Architecture and senior Go consultations agreed on a bounded registration/status
read for the existing Mini App and agent API. This records the selected contract;
implementation and acceptance are described [separately](shuttle-account-status.md).

Use one owner-only PostgreSQL repeatable-read snapshot, exact current bot,
organization, local user UUID, subject and nonrevocation. Include waitlisted and
assigned registrations, including fully paid, closed-sales and ended events.
Existing actionable-event discovery and unpaid expectations are incomplete account
indexes; authority discovery also includes other people's events. Do not reuse
those indexes as a claimed complete owner list.

Default five records, maximum twenty, stable event UUID keyset pagination and
32 KiB item budget. Each page has its own observed instant; pagination is not a
frozen cross-request snapshot. Event names, IANA timezone, dates and the owner's
registration role/state are safe projections. Disabled registration modules
retain those owned snapshots with `registration_enabled=false`, but omit the
pass/payment block. Unavailable money is neither zero nor unpaid.

For enabled assigned entries, reuse frozen pass terms, `paymentBalance` and
`PendingPassEvidence` on the same transaction queries. Show confirmed and remaining
amounts separately from pending evidence. Overdue requires an unpaid balance;
it never cancels the pass. Dates use each event's zone and amounts use integer
minor units with the frozen scale. No foreign registrations, authority queues,
reviewer IDs, evidence files, IAM claims or full administrative configuration.

Expose the same typed read through `GET /app/account` and authenticated agent
`registrations/list`. Mini App session/IAM checks precede the read; reread the
current session after it, before responding. Never hold a DB connection over IAM.
No self-HTTP, general internal-tool proxy or new authorization framework.

The UI lists these records and supports read-only pagination/retry. It does not
persist an event choice or claim a viewed event as agent business context. It
creates no conversation, Loom run, registration, payment, expectation or user
action mutation. Registration/payment forms and event-selection actions remain
separate work, including their durable agent context.

Real Telegram launch remains the deferred CA gate.
