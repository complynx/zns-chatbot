# Native order transactions and reactive context

The candidate now has native session wrappers for the same order preparation,
submission and cancellation code used by the
[agent API](shuttle-orders-submission-api.md).
This component implements the
store/session and recent-action parts of the
[agreed plan](shuttle-orders-submission-plan.md).
The native
[HTTP](shuttle-miniapp-order-submission-http.md) and
[UI](shuttle-miniapp-order-submission-ui.md) components now have separate gates.

## Session and shared business transactions

`PrepareMiniAppOrderSubmission`, `SubmitMiniAppOrder` and `CancelMiniAppOrder`
accept a deployment and a `MiniAppOrderSession` containing bearer hash and key
generation. They derive the owner from the session. Callers must verify IAM
before entry; the native HTTP layer now performs that check. The wrappers
introduce no new database table, workflow log or dependency.

The transaction first resolves the bearer without row locks, takes the same
owner-operation advisory lock as agent order calls, then locks event, owner and
session. It checks session state/expiry/key generation, current account identity,
bot/organization, enabled module and assigned-pass access before any receipt
replay or effect. Logout and account revocation serialize on the owner lock.
A write waiting for the event lock rechecks the session after that wait. No
IAM, Telegram or Loom call occurs while these locks are held.

Each wrapper calls the existing private transaction helper and commits only a
new successful write. Matching receipt replay rolls back its read-only
transaction. Prepare/cancel validation and request hashes are shared with the
agent entry points. Submission uses the same exact-proposal hash and canonical
owner/event/source-draft identity. The original business result stays immutable
across transport aliases, cancellation and a reopened store. A new Mini App call
records its trusted source as `miniapp`; replay of a receipt originally created
by another channel does not relabel that receipt or invent a native action.

Order, consumed draft and operation receipt remain atomic. Receipt failure
leaves no submitted order, no submission receipt, and no consumed draft. The
existing preparation remains usable after the failure is removed. No capacity
or confirmed money is created by this stage.

## Reactive action projection

`RecentMiniAppOrderActions` reads at most five existing native operation receipts
for this task's owner, newest first. It filters current bot/organization, module
and assigned-pass entitlement and excludes receipts whose timestamp is not
before the task's creation timestamp. A foreign task owner receives no history.
The projection contains only operation/event IDs, phase, optional proposal/order
IDs and UTC `recorded_at`; it does not include item labels, amounts, evidence,
profile details or session credentials.

`recorded_at` is the receipt insertion timestamp, not a promise of commit-order
linearization. The cutoff is the stored task timestamp. Later receipts do not
change that earlier task's window, but this bounded history is not a complete
workflow log and is not a current-state snapshot.

The profile context appends this metadata only when processing an explicit user
request. Native preparation, submission, cancellation and aliases do not start
Loom runs or Telegram deliveries. The prompt labels history as untrusted data,
not instructions or consent. It directs the agent to read the exact immutable
operation receipt and current order/draft separately. A preparation is not proof
of submission; history does not authorize another write, cancellation or clearing
of newer quantities.
