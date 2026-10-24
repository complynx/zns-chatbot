# Native order submission UI component

The existing RU/EN saved-selection form now supports reviewing and confirming an
order, listing submitted orders and cancelling an eligible exact order. This
uses the native HTTP contract in `shuttle-miniapp-order-submission-http.md` and
its shared native/agent preparations. It is a source/UI component milestone;
owned-stand deployment and actual Telegram acceptance remain required.

## User flow

Choose an event, edit quantities and save the selection. Unsaved edits disable
order review. “Review order” prepares the saved revision without submitting it.
The review shows the event name, all item and required-charge lines, quantities,
unit/line amounts, exact total, cancellation cutoff and proposal expiry. Dates
use the quote's event IANA zone; amounts use integer decimal formatting. Names
and other data are rendered with text nodes, never HTML. Offer IDs identify the
quoted service group; the quote snapshot does not yet retain service titles or
service start/end instants. The editor displays current catalog service times.

Only “Confirm this order” sends `confirmed: true` with that proposal and the
exact prepared versions. Editing a quantity invalidates the displayed review.
An expired proposal cannot be submitted. The server remains authoritative for
current revision, access, expiry and cutoff checks; rejection never silently
reprices or confirms a replacement.

“My orders” displays complete historical snapshots and current submitted/cancelled
state, exact order IDs and submitted times. Page controls follow the server's
returned cursor. An eligible submitted order has an explicit targeted cancel
button carrying that order ID and revision. Submission does not reserve capacity
or confirm money. Cancellation in this stage has no money or capacity effect.
Changing the selected event clears prior-event history and controls before reads,
so a failed new-event request cannot leave old orders actionable under it.

## Recovery and session behavior

Before any prepare/submit/cancel POST, persist its exact owner/event/kind/body in
`sessionStorage` under `zinusya-orders-operation-v1:{owner}`. It holds operation,
proposal/order IDs, versions and the explicit submission flag, not credentials,
private registration fields or a copy of the quote. The existing selection-save
record remains separate; unknown order work blocks selection edits and event
switching. Malformed/conflicting records block new writes, and storage failure
prevents dispatch. These tab-session records are recovery aids; PostgreSQL owns
the durable operation/effect.

On reload, query only the pending operation's exact phase receipt. A 404 means
unknown, not non-execution; retain the original body and offer explicit check or
identical retry. A recovered preparation is displayed for review and never
submits automatically. A completed submission/cancellation receipt is historical;
refresh the current draft/order list separately. Recovery does not clear newer
quantities saved by an agent. Cancellation recovery also reloads the current
selection so editing can continue. A definitive 400/409 response discards the
rejected request and requires explicit current-selection reload before another
preparation. Session denial clears rendered values but retains the original
owner-bound recovery record. Epoch guards ignore late responses after logout or
owner change. Another owner's start never queries the previous owner's receipt.

The form neither starts an agent nor interprets free text. Native writes project
trusted recent-action metadata into the next explicit user task, as documented
in `shuttle-miniapp-order-submission-store.md`. The agent can use the same proposal
and receipts. Full four-path behavior still needs actual owned-stand acceptance.
