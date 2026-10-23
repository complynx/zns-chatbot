# Native payment-review entry and reactive context

Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

## Source behavior

`/payment_reviews` is a structured command. It needs current verified IAM identity
and event reviewer access, but does not call Loom. Eligible reviewers also see
it in `/menu`. It opens the Mini App reviewer section; an ordinary participant
gets an access explanation. The HTTP API checks authority again for every read
and decision. A disabled form gets a localized unavailable reply.

New submitted-proof notifications carry a Mini App link with event/evidence IDs.
The link selects authorized evidence, including events outside the first page.
It carries no approval or session credential. Opening or forwarding it cannot
confirm a payment. A pending uncertain operation in the same owner's browser tab
is recovered before a different notification target is opened.

Completed Mini App call receipts form the action history; there is no extra
journal table. At the next explicit agent task, at most five earlier receipts
are added, newest first. Fields are operation, event, evidence, workspace IDs,
immutable workspace revision, kind, original outcome and completion time. No
amount, free-text reason, file bytes or session credential is included. Workspace
replacement inserts a new UUID/revision row; earlier revisions are not updated.

The read is restricted to the current actor, deployment bot/organization,
enabled registrations module and current event reviewer authority or verified
global administrator role. Revoked event access removes its context. Tasks with
no native review history avoid an additional IAM role check. Form calls never
create or resume a Loom task by themselves. The prompt marks history untrusted
and explicitly denies treating it as consent or a new instruction. Continuing a
user request requires reading current authorized workspace/operation state.
