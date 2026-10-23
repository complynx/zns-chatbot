# Native order payment reviewer form

It uses
mock Telegram and the actual shipped form/backend; the earlier real WebView input
barrier remains separate.
Fault/outage, unattended model and D23 money-policy
questions remain open.
Component versions below are historical milestones.

The Mini App includes a separate order-payment reviewer panel.
It uses the
[review read API](shuttle-miniapp-order-review-reads.md) and
[review write API](shuttle-miniapp-order-review-writes.md); pass payment balances
and editable pass-credit amounts are not reused.
Runtime identity remains zinusya.

## Review and handoff

Choose an authorized event and an unresolved proof. The panel shows the payer,
immutable order total, current proof state and event-local file/deadline times.
The server derives review permissions. Proof loading uses the current Mini App
session and the evidence-scoped content route. The download link refers to a
revocable browser blob, not a Telegram locator or IAM token. The form requires
loading the exact proof before final confirmation; actual visual inspection is
still the reviewer's responsibility.

Preparing saves intent without confirming money. The complete immutable workspace
preparation description is shown as literal text, including the sold order terms,
amounts and reason. Final confirmation repeats that exact proposal and decision.
There is no editable credit amount. Pending claims can confirm or reject;
exhausted and needs_decision claims cannot confirm money through this panel, but
can be rejected or left for a later human decision. Rejection does not itself
transfer a refund. Cancelling a saved decision changes intent only; it does not
cancel the order or its proof claim.

The form opens agent-prepared workspaces. Edits disable confirmation until a new
revision is prepared or the current revision is reloaded. Completion by the agent
appears on reload and prevents another new confirmation. Current detail and the
original native request receipt are separate views; switching event or proof
clears the prior receipt and status. The exact-ID link
#order-review?event=UUID&evidence=UUID can open a reviewed or off-page proof.
Discovery and queues use bounded pages of at most20 entries.

## Recovery and presentation

Before each save, cancellation or confirmation, sessionStorage receives the exact
owner/target/phase/body/operation UUID; the controller verifies read-back before
sending. This recovery record has business intent, including the reason, but no
IAM token or Mini App bearer. The surrounding app manages its existing session
separately. Records are local to the browser session; this is not cross-device
recovery. Server operations and workspaces remain durable.

Uncertain requests block replacement. Reload reads the original operation;404 is
unknown and never evidence that the write failed. Only explicit retry resends the
same body and key. A completed original receipt is checked for source, operation,
kind and target before clearing recovery state. If the agent completed the shared
canonical decision but no native receipt exists, explicit same-key retry recovers
that canonical outcome. Invalid or unreadable startup recovery storage blocks new
writes. The original result does not assert that a newer workspace is unchanged.

Logout and target changes revoke proof URLs and clear file names/content. Epoch
checks suppress late replies after logout/owner changes. EN/RU language changes
update labels, state and receipts without rewriting the immutable description or
pending request. Event times use IANA timezones and retain DST offsets. User and
server strings are rendered with textContent, not HTML.
