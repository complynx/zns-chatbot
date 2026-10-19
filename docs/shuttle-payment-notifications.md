# Payment notification delivery (in progress)

Migration 00013 adds a durable payment notification queue without changing the
confirmed ledger or existing agent tasks. Apply migrations before exposing the
candidate. Older binaries do not create or drain these notices; do not mix
writers during rollout. Rollback to 00012 retains the payment ledger, but export
notification documents/outcomes before intentionally dropping the new table.
No historical payment notification backfill is performed.

Evidence submission atomically creates submitted notices for the currently active
ambassadors/event administrators in this event and deployment. No current recipient
means no push notice; the evidence still exists in the authorized review queue.
A later authority grant does not send an unsolicited backlog automatically.
Confirmation/rejection atomically creates a decision notice for the evidence owner.
Replaying the original payment operation does not recreate either kind of notice.
No notification credits money or starts a proactive Loom run.

Documents preserve the event name, IANA zone, evidence, decision and balance as
business snapshots. Amounts use exact frozen pass currency/scale. Decision messages
identify the decision time and qualify remaining balance/status as the state
immediately after that decision; concurrent/retried delivery need not be chronological.
They do not advertise an old deposit as the current balance after a newer top-up.
Plain text treats names/reasons as data. System messages use ru/en with be/uk -> ru,
other languages -> en, and the configured default for absent Telegram language.

The application repairs River jobs from committed notices in bounded UUID pages.
A cyclic in-memory cursor prevents 100 older IAM-waiting rows from hiding later
healthy recipients. Jobs contain only notification ID and version; the durable
notice is the source of truth. Restart can repeat a scheduling sweep safely.
Recipients and source owners must still be active in this bot/organization.
Submitted evidence must still be pending and the recipient must hold current
ambassador/administrator authority for that event. A reviewed submission alert is
blocked as obsolete. Removing the registrations module blocks delivery too.

The sender obtains/verifies the recipient's user token, then checks local event
access again while reserving the notice under the event lock. This is a current
check before IO, not a claim that revocation can retract a message already in flight.
Token/transport outages before send retain the pending notice. Confirmed Telegram
rejection with a retry delay schedules a later attempt; permanent rejection is
visible as delivery_failed. The reservation and exact payload are committed before
network IO. Uncertain send or crash during send becomes delivery_unknown and is
not automatically resent. Successful receipts persist message/chat IDs.

The user permits a duplicate message only as a last resort when delivery cannot
be established and completion requires repeating it. Inspect stored receipts and
the actual chat first where available; record the reason and possible duplicate.
This must never replay the underlying payment, registration or agent write.
Migration 00014 adds immutable operator proposals and resolution receipts. The
candidate implements audited retry/dismiss; representative native Telegram QA passes.

## Agent diagnostics

Current global zinusya.admin authority is required for both methods in
zinusya_administration (zero-TTL authenticated discovery):

- notifications: optional state/after/limit; UUID metadata pages, default/max 20.
- notification: id; frozen business document, recipient, delivery state/version,
  attempt ID, Telegram receipt and timestamps. Credentials/payload bytes are excluded.

Paths: /internal/agent/administration/notifications and /notification.
Reads use the ordinary protocol_version/operation_id/arguments envelope.
Listing does not return financial documents; detail is separately authorized.
Foreign deployment IDs are not found. A missing receipt is not proof that Telegram
never received the message. Metrics use the bounded notifications component label.

The shared operator resolution also supports pair invitations (`notification_kind=pair`)
and [order evidence notices](shuttle-order-notice-delivery.md)
(`notification_kind=order`). Omitting the kind retains payment compatibility.
Their business events and source authorization policies remain separate.

## Operator resolution

The `resolve_notification` method in `zinusya_administration` requires current
global `zinusya.admin` authority and an active actor in this deployment. Its arguments
are event_id, notification_id, expected_version, action (`retry` or `dismiss`) and
a nonempty reason (maximum 512 bytes after trimming). It accepts only unknown,
failed or blocked delivery states. Successful or in-flight sends cannot be retried.

POST /internal/agent/administration/resolve_notification/prepare creates a persisted
five-minute exact proposal, bound to actor, operation ID and normalized arguments.
It discloses recipient, event, state/version and possible duplicate for an unknown
send. Preparation does not send a message or change the queue. Exact preparation
replay preserves the original expiry; use a new operation after inspecting an
expired or stale proposal.

POST /internal/agent/administration/resolve_notification requires the same arguments,
operation_id=idempotency_key and confirmation mode `exact_parameters` with the
returned proposal_id. Retry creates one new pending delivery generation; the worker
checks current recipient/source/event access before sending. Dismiss records a
terminal queue state and does not assert Telegram delivery. Dismiss remains possible
when the event module is disabled. Ledger, evidence decisions and passes are unchanged.

The operation atomically stores its original result, previous state/Telegram receipt,
actor, reason and timestamp. Exact replay returns that result after expiry, delivery
or restart. Changed arguments or proposal identity conflict. POST the same operation
ID to /resolve_notification/status or /resolve_notification/reconcile to retrieve the
original receipt. `not_found` means no committed receipt was observed; it does not
prove that a message was never delivered. Neither endpoint sends anything.

Apply 00013 then 00014 before exposing manifest mvp-12. Older binaries do not expose
operator resolution. Binary rollback retains both tables. Export proposals, receipts
and notifications before any intentional schema rollback, in reverse migration order.
No historic notification rows are created by these migrations.
## Acceptance boundary

Focused HTTP/PostgreSQL/Telegram-peer race tests pass for queued submission,
operation replay, restart, owner deposit status, ru fallback, superseded alerts,
rejection messages, source/recipient revocation, IAM downtime, Telegram retry_after,
unknown receipt without automatic resend and repair past 100 waiting recipients.
Two independent reviewers found scheduler starvation and an unqualified historical
status; both were fixed with focused behavioral assertions. Fresh independent
review found no actionable defects. The full Linux/PostgreSQL race suite and lint
passed after those fixes. The subsequent shared page-limit binding passed focused
notification and fairness race tests, lint and a fresh independent review.

Focused resolution race tests pass for last-resort retry, dismiss, scoped access,
expired/changed proposals, original receipt replay after delivery/expiry/restart
and no payment credit. Two independent source reviews found no actionable defects.
The final full Linux/PostgreSQL race suite and zero-issue lint passed for mvp-12.

Native submission/confirmed notifications, queue recovery through simultaneous
IAM/Loom downtime and bot restart, and real Loom exact dismiss approval/status pass.
Unknown-receipt dismiss used an explicit SQL fixture after visible Telegram delivery;
native retry was not attempted for that known-delivered message. See
[native QA and its limits](shuttle-notification-qa-2026-10-14.md).
PDF rendering remains a separate failed gate (Loom issue #4). No production rollout
or historical payment import is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
