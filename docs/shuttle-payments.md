# Payment evidence and confirmed ledger

Payments use the assigned pass's frozen exact minor amount, currency, scale and
initial/final deadlines. Event edits do not rewrite existing pass terms. Submitted
evidence and confirmed money are separate: an image, amount claim or agent
classification never marks a pass paid.

## Submission and access

The authenticated owner submits event_id, pass_id, opaque file_id, reported amount,
currency/scale and purpose (initial or top_up). The file must belong to the same
active local account. Its persisted Telegram sent_at is the arrival time used for
deadlines; the agent cannot supply or alter it. submitted_at records processing
separately. Timely means sent_at is strictly before the applicable frozen due_at.

Submission creates a pending immutable record and shares that proof with the event's
current payment authorities. It credits no money. Pending/confirmed Telegram source
provenance cannot fund another expectation for the same owner, even after a new
upload of that source. Rejected evidence can be corrected and resubmitted under a
new operation; earlier evidence and its decision remain. This is operational
provenance deduplication, not bank-transaction or cross-user fraud detection.

The registration module must remain enabled for the event. Assigned unpaid passes
remain assigned after deadlines. There is no automatic cancellation or forfeiture.

## Original receipt reads

The source candidate `zinusya/mvp-75` adds the read-only agent method
`zinusya_payments.operation` at `/internal/agent/payments/operation`. Its
`arguments.operation_id` is the original payment operation locator; the outer
read request has its own operation ID. No event hint or new write is needed.

The result contains `receipt`, with `kind:submit` and the complete original
`evidence`, or `kind:review` and the original legacy `review` result. A submission
receipt stays pending after later human confirmation; read `evidence` and
`balance` for current money. Missing or another actor's operation returns
`receipt:null`, which is unknown rather than proof of non-execution. Never
replace the operation or resubmit its file solely because a receipt is missing.

Reads reuse the existing actor/bot/organization-scoped operation lookup and
recheck current event/evidence access. Event authority does not expose another
actor's original receipt. Reading a historical receipt creates no evidence,
credit, consent or renewed preparation. Shared review-workspace transport
receipts still use `review_operation`, with their separate canonical operation.
This operation read is installed on the owned schema64/mvp78 primary.

## Human decisions

The actor must be the current event ambassador/administrator or a global
zinusya.admin. Rights are checked at each operation in the same event lock used for
role changes. An assigned pass alone does not grant review rights.

A confirmed decision credits a positive exact amount, at most the remaining frozen
price. A rejected decision credits zero and requires a reason. The human may correct
the owner's reported amount: preparation shows both values. A late proof or a top_up
without an established deposit requires an explicit case-decision reason. An earlier
initial proof may remain pending while the top_up awaits its review; calling the
first late transfer a top_up cannot silently bypass the initial deadline.

Timely proof may be reviewed after sales closing or event ending. Review proposals
expire after five minutes independently of sales closing. Prepare reserves nothing
and records no money. Execute requires exact approval bound to actor, operation,
arguments and proposal. It rechecks authority, evidence, balance version, remaining
amount and expiry after locks. Decision, ledger revision and original receipt commit
atomically. Concurrent confirmations from the same balance version allow one effect.

The [payment-consent source fix](shuttle-payment-consent-presentation-2026-10-20.md)
uses the original actor-owned workspace preparation for agent questions, even if
Loom supplies an abbreviated prompt. It displays human terms without raw argument
JSON and rechecks exact parameters, current rights, availability and expiry before
delivery. If that decision already completed through either interface, the question
says so and retains the original terms for reconciliation; replay credits no new
money. This fix has passed component tests, source review and the
[packaged owned IAM/Loom runtime gate](shuttle-payment-consent-runtime-qa-2026-10-20.md).
The fix is now [installed on the owned primary](shuttle-payment-consent-primary-qa-2026-10-20.md),
with fixed/original/fixed binary rollback, unchanged data/queues and ingress
recovery verified. This is a local stand installation, not production deployment.

Confirmed review rows form the append-only money ledger. Rejected decisions remain
as audit rows. Balance exposes confirmed_minor, remaining_minor, deposit_required_minor,
deposit_satisfied and fully_paid separately. A zero-minimum deposit policy needs a
positive confirmation for a priced pass; free passes need no bank transfer. Without
a deposit policy, full confirmation satisfies the payment expectation.

Evidence detail/list/queue includes the optional persisted review: actual credited
amount, reviewer ID/role, reason and review time, distinct from the reported amount.
Other authorized reviewers can audit a prior decision without impersonating its actor.

Overpayments beyond remaining price are not silently credited. Refunds and corrections
to an already confirmed entry need a later explicit workflow; no bank transfer is
performed by these APIs.

## Agent tools

The dynamic zinusya_payments module uses authenticated discovery with zero cache TTL.
Owners receive events, balance, evidence, list, submit and fetch. Event authorities
also receive queue and review. Execution checks the specific event again; a role in
one event gives no access to another. Global administrators can review this deployment.

- events: UUID-paged event choices with dates/timezone and optional own_pass terms.
  Assigned payment expectations remain discoverable after sales closing. Other users'
  pass details are excluded. Select explicit/pinned context or the nearest relevant
  registered event; clarify multiple plausible expectations.
- balance/evidence/list: current permitted reads; page size defaults to 20, maximum 20.
- submit: durable owner write, no money confirmation; idempotency_key = operation_id.
- queue: all submitted evidence/decisions for the authorized event, in UUID order.
- review: exact_parameters prepare/write/status for the human's decision.
- fetch: explicit proof import into Loom by external_id = evidence ID, not foreign file ID.

Base path: /internal/agent/payments/. Write status paths are submit/status and
review/status; prepare is review/prepare. Successful operation receipts remain
original snapshots after later changes. Missing status is unknown, not proof of
non-execution; reconcile the same operation rather than replacing it blindly.
Known business rejection is terminal_rejection; uncertain storage returns 503.
Authentication/permission failures retain 401/403 rather than becoming storage errors.

Proof fetch declares external_disclosure=true for permitted agent processing of
the submitted proof. Loom's live user/run policy must still allow disclosure, and
only the owner/current event authority can obtain the source. This flag grants
neither financial authority nor access to another file.

Proof artifacts use /internal/payment-evidence/{id}/content. The source owner or
current event authority can read only that submitted association. Telegram read is
bounded; authority is checked again after remote IO and on download. Safe attachment,
nosniff, sandbox and private/no-store headers apply. General file APIs remain owner-only.
Source loss does not erase evidence or money history. Already imported bytes have
Loom's own lifetime; revocation prevents new service reads and financial effects.

## Migration and acceptance boundary

Migration 00012 adds evidence, append-only decisions, ledger versions, proposals
and receipts without importing historical money or changing existing passes/users.
Apply -migrate before exposing the new API. Export these records before intentional
down migration. Historic registrations/payments remain a later archival import.

Behavioral HTTP/PostgreSQL tests cover pending/no credit, deposit/top-up, exact
approval/replay/restart, current rights, source dedup, corrected rejected proof,
explicit late decisions, concurrency/cap, proposal changes/expiry, actual review
amount auditing, closed event discovery and revocation during source IO.
Full Linux race QA passed with event discovery included (integration: 70.064 s).
A fresh independent textual review found no actionable findings. Final lint passed
with zero issues; focused payment race tests passed after the lint-only extraction.
On the newly provisioned owned stand, real IAM delegation and native Loom exact
approval confirmed a 3087-minor-unit deposit against a 12345-minor-unit frozen pass.
Pending evidence credited zero, owner self-confirmation was denied, and bot restart
preserved the original operation status, deposit balance and exactly one ledger row.
This native scenario used synthetic closed attachment metadata and a manual Codex
mailbox provider with the isolated Loom seal fixture; no real bank document was read.
Durable ambassador/owner notifications and scoped diagnostic reads are now in
progress; see [notification delivery](shuttle-payment-notifications.md). Audited
operator retry/dismiss is implemented and has passed focused race QA and source
review. Representative real Telegram notification/restart and native Loom dismiss
approval QA now pass; see [native QA limits](shuttle-notification-qa-2026-10-14.md).
Real synthetic Telegram file upload/read is accepted separately in shuttle-files.md;
this does not establish bank receipt understanding or production readiness.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

### Native correction and final payment follow-up

Real Telegram PDF upload, owner-scoped pending top-up, exact rejection, explicit
same-file resubmission, exact full-payment confirmation and restart receipt checks
passed on the owned stand with synthetic money. See
[payment correction/top-up QA](shuttle-topup-qa-2026-10-14.md) for provider, explicit
context, byte-integrity and remaining classification limits.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
