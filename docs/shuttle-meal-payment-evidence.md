# Shared meal payment evidence

Accepted source snapshot: migration69 / mvp-88, verified in bounded Linux
batches.
This is not an installation or
complete meal checkout acceptance: financial meal creation tools/forms were still
pending, together with full end-to-end runtime QA.

Earlier component counts below remain historical evidence.

## One payment engine, persisted business kind

Service and meal orders share evidence, review workspaces, decisions, receipts
and notifications. Migration69 expands the evidence kind check to
service|meal and adds an immutable full-consent text field; the existing composite order/user/event/kind foreign key continues
to protect identity. Its historical order_evidence_service_identity name is
retained. The default remains service for old writers. Rollback refuses to remove
meal evidence or separate consent presentations. An existing service-only database
with no new separate presentations can roll back/reapply without rewriting payment
history or receipts. New service review presentations also block destructive rollback.

The existing order-payment API accepts an explicit order/evidence target. It
resolves the persisted kind itself; callers cannot switch modules by sending a
kind value. The owner needs a current assigned pass, including unpaid, and the
matching module: orders for service or meals for meal. Specific reads, receipt
recovery, file access and review/commit recheck that module. Lists filter each
row by its kind before applying the page limit and cursor. Enabling another
module grants no access to a disabled kind.

Review requires current authority for that event and matching enabled module.
No reviewer pass is required. A revoked payer cannot read the proof, but their
revocation must not prevent an authorized ambassador from resolving it. Native
sessions are checked inside the event/actor transaction, including replays and
calls released after lock waiting. File reads retain their before/after external
IO authorization checks.

## Arrival, review and immutable outcomes

A meal proof sent before the frozen common meal payment deadline remains timely
when processed later. Telegram metadata supplies sent_at; browser callers cannot
supply a fake arrival instant. Sending exactly at or after the deadline requires
a human case decision. Existing service-order admission retains both its arrival
and processing cutoffs. Human review itself need not occur before the evidence
deadline; its own exact proposal still has the normal short approval expiry.

Only an exact full-price timely claim enters pending. A different amount or late
proof is retained as needs_decision, with no automatic confirmation. Unresolved
claims can be rejected here; they cannot be directly confirmed by this path.
Meal capacity is explicitly unlimited and creates no limited-resource holds.
The existing source-file reuse and one-active-claim checks are shared with
service orders and pass payments. No proof submission moves money, fulfills an
order or performs a refund.

Native and agent calls recover the same immutable submission receipt after
reconnect or human review. It can still say pending while a current-state read
says confirmed. Workspace edits, authority removal or module removal invalidate
execution access. A successful canonical decision is reused rather than applied
again; current access is still mandatory for recovery.

## Full consent and notifications

The review summary renders the frozen meal days, service times in the event
IANA zone, selected names and original charge lines. Combo components are
shown as composition; they are not charged again at individual item prices.
Individual names occur on charge lines, avoiding repeated labels.
Names and reasons remain literal untrusted data. No current catalog replaces
sold terms in a review.

Loom limits preparation descriptions to4000 bytes. A larger review therefore
uses a bounded transport description identifying the exact event, evidence,
workspace, revision and decision. The complete immutable description is stored once as text on the canonical
preparation, with a separate256KiB bound. Compact JSON receipts restore that exact
text on authorized DB/native reads without changing their original state/revision.
Older embedded-text preparations remain readable. Agent workspace, operation and
status responses also bound their presentation descriptions. The existing specialized
bot presentation reads the complete authorized receipt and delivers all chunks before
the final consent keyboard. No full
composition or monetary detail is truncated from the human consent record.

Notification authorization and native payment/review history also use the
persisted kind. Meal notices describe pending, confirmed or rejected payment
without claiming resource holds. Notifications are durable delivery work;
Telegram delivery does not decide payment state.
