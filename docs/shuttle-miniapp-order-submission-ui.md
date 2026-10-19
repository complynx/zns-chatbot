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

## Evidence and limits

The final Node VM gate passed all 64 behavioral tests across profile/private
registration fields, saved selection, new order submission and payment-review UI.
The orders file now contains 23 cases, including exact review/explicit submit,
lost committed reply with a newer agent draft, unknown status/identical retry,
preparation recovery without auto-confirmation, unsaved/stale versions, storage
failure, owner isolation, list pagination/RU labels, failed event switching,
cancellation recovery, expiry and a late successful reply after logout.

Two independent reviewers found the same event-switch history defect. Its failing
test output was retained before the fix. Root also reproduced a cancellation
recovery gap: the receipt/list recovered but the current selection was not
loaded, leaving editing disabled. The fix refreshes that current state too;
its failing output is retained separately. A fresh full textual review of the
final diff reported no actionable findings. Reviewers ran no code or UI.

Evidence: `.cache/shuttle-work/orders-submit-ui-review-v1/` retains failures and
post-fix runs; `.cache/shuttle-work/orders-submit-ui-v2-qa/` retains final TAP,
stderr and unchanged source hashes. No test was skipped or failed in the final
gate. No containers were created by these VM tests. `go test ./internal/api`
also passed during UI development before the final event-switch/recovery fixes;
it is not claimed as a repeat against those later JavaScript-only changes.

These tests simulate the DOM and backend. They do not prove actual browser
layout, actual HTTP interaction by this form, IAM/Loom availability behavior or
Telegram Mini App execution. The full release Dockerfile now builds image
`sha256:82c1aaeeb58fcb51ad3ace91c3c3f8e3da6c461ea8a0d5dcb0aceaef9209652a`
from 753 frozen build inputs; source hashes remained unchanged. Its dedicated
2GiB/no-swap, 2CPU builder and cache were removed. This exact image migrated an
isolated schema39 snapshot to40, preserving all existing table contents and
creating three empty preparation/order/receipt tables. Clone PostgreSQL and
migrator were removed with no persistent test volume; the main ten service
fingerprints remained unchanged. Build and migration receipts are retained in
`.cache/shuttle-work/orders-submit-images-qa/` and
`.cache/shuttle-work/orders-submit-schema-qa/`.

Reviewed owned-stand activation and native-only actual Telegram save/review/
submit/cancel, RU/EN rendering and clean bot-restart history with Loom down later
passed; see `shuttle-orders-submission-real-qa-2026-10-17.md`. Agent-only, both
mixed paths and real ambiguous-commit recovery remain pending. Full order proof,
capacity/payment review and meal fulfillment remain later stages. Production
readiness is not established. No commit, push, PR or production deployment was
performed; the index remains empty.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
