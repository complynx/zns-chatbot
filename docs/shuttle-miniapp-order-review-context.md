# Reactive native order review history

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Successful native review requests are now available as bounded historical metadata
in the next explicit user-triggered agent turn. This extends the
[native reviewer form](shuttle-miniapp-order-review-form.md), without starting
Loom from a form action. Source schema42 / manifest mvp-43 is unchanged. The owned
main stand remains schema40/mvp-40; this source is not yet activated there.

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

## Verification and limits

The final focused Linux/PostgreSQL race gate passed59 roots /123 pass events:
13 integration roots /20 events,33 database roots /61 events and13 task roots /
42 events. It included native order-review reads/writes, the three new context
roots, existing pass-context regression paths and DB/task tests. No failures,
skips or OOM occurred. All 925 frozen inputs matched; the ten main-service
fingerprints were unchanged. Go was limited to2GiB and disposable PostgreSQL to
512MiB; their exact containers and volume were removed. Proof is retained in
ignored .cache/shuttle-work/order-review-context-v2-qa/.

Three added behavioral roots cover bound5/cutoff, redaction, agent-only exclusion,
foreign task/deployment isolation, role loss/global-admin/module filtering,
original prepare/cancel/confirmed outcomes after exact retry and store reopen,
one occupied allocation, and explicit user-turn prompt delivery after service
restart. The latter observes zero agent runs before a Telegram user message and
no review history after role removal. These use real Go/store/PostgreSQL with
mock IAM/Loom/Telegram peers. They do not prove real-service manual acceptance or
hard-kill/transport-disconnect recovery.

The initial gate also passed59/123, but its lint found four dupl issues. Those
artifacts remain in v1. Shared actor-lock setup and pass/order context assembly
were consolidated; order scanning uses pgx.CollectRows and keeps its UUID typing.
Domain SQL/authorization rules and original pass result paths remain separate.
Final scoped lint reports0 issues. Initial correctness/quality reviews and the
fresh post-refactor source review found no actionable defects. Their assumptions
about workspace revisions and tool names were independently checked against the
SQL and manifest by the root agent. No changes were staged or published.

[Owner proof acceptance history](shuttle-miniapp-order-proof-context.md) now has
separate schema43/source verification through an additive native journal. Old
agent submissions are not bulk-relabeled. Authorized durable order notice delivery,
owned image/migration activation and real native/agent/mixed/restart/fault QA are
also pending. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
