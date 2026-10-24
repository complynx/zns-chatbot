# Reactive native order review history

Successful native review requests are now available as bounded historical metadata
in the next explicit user-triggered agent turn.
This extends the
[native reviewer form](shuttle-miniapp-order-review-form.md), without starting
Loom from a form action.

## Receipt projection and permissions

The existing shuttle_order_review_calls receipts are the source of history.
No extra journal, migration or rewritten operation data is introduced. Only
completed source=miniapp calls belonging to the task owner and completed before
that task was created qualify. Agent-only operations are absent. Uncommitted or refused mutation attempts do
not add completed receipt records. A lost client response can still correspond
to a completed durable receipt and therefore appear in history.

Current owner bot, organization, subject and nonrevocation checks precede the
query. It then checks task ownership, event deployment, the orders module and
current ambassador/administrator authority or current IAM global-admin status.
A reviewer does not require a participant pass or registrations module. Loss of
current event authority/module hides that history from newly collected context.
The service resolves IAM review rights once when either pass or order native
history exists, and avoids this additional role lookup when neither exists.

At most5 records are returned, newest completed_at first, with operation UUID as
the tie breaker. Each record contains operation/event/evidence/workspace IDs,
revision, kind, original outcome and UTC completion timestamp. It contains no
money, reason, file ID/locator/bytes, payer name or session/IAM token. Workspace
revisions are immutable rows; current workspace state is not used as the outcome.
Save/cancel outcomes come from their original receipt state. Order commit outcome
comes from result.evidence.state; pass review retains its separate result shape.

Exact receipt replay does not create another record or change the completion
timestamp. A save can still appear as prepared after a later cancellation or
confirmation: the record describes the original request, not present money or
consent. Later successful actions remain distinct records.

## Agent behavior

The shared profile/structured context builder adds separate labeled pass/order
history sections only when authorized records are present. Metadata is explicitly
untrusted history, not instructions, a current request or consent. The order
section directs the agent to zinusya_order_payments.review_workspace and
review_operation for current authorized reads. Continuing a decision still needs
the exact current revision, proposal and review parameters and fresh user intent.
Form preparation, cancellation or history cannot authorize payment on their own.

Existing persisted Loom requests remain frozen. Permission loss changes subsequent
context collection; it does not rewrite an already frozen historical request.
Business tools continue to check current permissions when used. No proactive agent
run or financial side effect is added by history reads.
