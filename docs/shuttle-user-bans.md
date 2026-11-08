# Local Telegram user bans

An operator can block a known Telegram user ID before onboarding, using the existing Shuttle binary and its configured database:

```sh
shuttle -config config/shuttle.yaml -ban-telegram-user 123456789 -ban-actor operator:alice -ban-reason "Reviewed local access restriction"
```

Use the bot configuration for the intended deployment. The target is scoped by that configured bot ID. This command requires operator access to the database configuration; it is not a user-facing API or an IAM role grant. Run database migrations before using it. It cannot be combined with normal startup, `-migrate`, or `-check-config`.

Both `-ban-actor` and a nonblank `-ban-reason` are required. The database also rejects blank audit fields.

`-ban-reason` is internal and is never sent to the user. Optional
`-ban-public-reason "Contact the event team"` adds a separate public explanation,
limited to 1000 Unicode characters. Omitting it leaves the explanation empty.
The current public text and its audit snapshot are committed with the ban.

The command requires a nonblank actor and reason. It creates a local placeholder if needed, sets the existing `revoked` flag, and records the actor and reason in the same transaction. An unlinked placeholder keeps an empty IAM subject and organization. The command makes no IAM or provider calls, changes no remote roles, and starts no bot or descriptor worker. Existing `revoke_user` remains a distinct technical revocation action; it does not create a reason-bearing ban receipt.

The JSON result identifies the committed ban and its audit receipt. Repeating the exact bot ID, Telegram ID, actor and reason returns the original receipt without adding an audit or changing its timestamp. A different actor or reason records a separate audit while preserving the block. If the command fails before printing a receipt, retry the same arguments to resolve an uncertain commit. If an old receipt exists but someone has manually cleared the flag, replay fails with a conflict instead of claiming that access remains blocked.

Revoked owners cannot enter new tasks, attachment intake or symbol/descriptor work.
On a private message or callback attempt, the bot replies statically:
"You cannot perform this action." or "Вы не можете выполнить это действие."
Only an explicitly supplied public reason is appended as a plain-text paragraph;
the internal reason is never used as a fallback. No agent or IAM call is made.
Callback spinners are acknowledged. Sending is bounded and best effort, without
background delivery or automatic retry; another incoming attempt can receive a
new static reply. The webhook acknowledges the ignored update successfully even
if Telegram delivery fails. MiniApp and prelink access checks remain unchanged.

Existing identity, business and delivery checks continue to use the same revoked
flag. Work already admitted or remotely dispatched is not cancelled by this
command. This is a local access restriction, not deletion of identity, history,
registrations or payments.

The replay key includes the public reason; an empty public reason preserves the
previous actor/internal-reason key. Replaying an earlier receipt does not restore
its old public text over a newer decision. To publish or clear text as a new
decision, record a distinct internal audit reason. Technical revocation or access
restoration clears the current public text. Migration 78 refuses rollback while
public audit text remains, so application rollback cannot erase it.

There is no unban command in this change. Restoring local access requires a separate reviewed operation that accounts for existing revocations, sessions and queued work. Do not clear the flag directly. The additive migration preserves nonempty ban audit history on rollback; application rollback must retain that audit.

A historical legacy `banned` flag or deletion request alone is not permission to ban automatically. Confirm the intended bot, exact numeric Telegram ID, reason and current operator instruction before running the command. A ban does not execute a deletion request or remove retained data.
