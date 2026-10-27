# Owner order payment proof form

## User flow

Open Orders, select an event, then use **Payment and proofs** on an existing
order. The panel displays its frozen event, order UUID, total and evidence deadline
in the event timezone. A zero-total order remains readable. Reported payment
amounts must still be positive; an exceptional amount follows existing admission
rules and does not silently confirm money.

Send an attachment to this Telegram bot first. Select its accepted file metadata,
load that exact file, check it and enter the reported amount. Loading enables
submission; changing files requires another load. Comma or decimal point is
accepted. Conversion uses integer arithmetic, the frozen currency scale and the
existing money bound; zero, negative, exponent and excess-precision inputs fail.

A listed file is not automatically classified as payment evidence. The explicit
order/file submission creates that claim. No file bytes are sent to Loom. Source
loss in Telegram can make later reads fail without deleting the retained claim.
Downloaded bytes are offered through a temporary blob link; this form does not
render arbitrary HTML or embed a PDF viewer.

Current proof history includes claims made through the agent. Each claim has its
own **Load this proof** control, independent of the disabled new-proof fields.
An active claim, unknown history, or an unvisited history page blocks a new claim.
A rejected-only history can permit another explicit submission. The original
submission receipt and current human decision are displayed separately.

An order's general list label says to check its payment proofs. Submission alone
cannot determine whether a later ambassador decision confirmed money or held
capacity. The proof panel uses the shared admission states. It never confirms
money, cancels a financial claim, or performs a refund.

## Attachment route

GET /app/orders/{event}/proof-files/{file}/content requires the existing owner
Mini App session and current orders entitlement for that event. Deployment and
owner come from the session. Another user's file is not readable by knowing its
UUID, including when the caller is an ambassador.

The store validates the active session after event-lock waits, loads the owned
file metadata, then releases the transaction before Telegram IO. Before serving
bytes, the handler refreshes identity/session authorization, repeats entitlement
and ownership checks, and compares the file UUID and Telegram locator. Logout,
module removal or a changed locator prevents stale source delivery.

The response has attachment disposition, detected content type, nosniff,
private/no-store caching and a sandbox CSP. The existing file timeout and bounded
read apply. IAM credentials and Telegram locators are not returned to the browser.

## Recovery and browser lifetime

Before POST, save the exact owner/event/order/file/version/amount/operation request
in sessionStorage and verify the write. Storage failure prevents a new POST.
After a lost or malformed response, keep that record and block replacement.
Reload checks the original operation. A missing receipt means unknown, not that
nothing happened. Only an explicit retry sends the same operation and body.
A definite 400/409 rejection follows existing order-form recovery: reload current
state before allowing new intent. A successful historical receipt does not replace
the current claim state.

Recovery records are owner-bound. Invalid or foreign records block new writes.
Session expiry requires reopening from Telegram; the exact pending request can
then be reconciled. Clearing browser session storage loses local recovery context;
current claim history still prevents an active claim being silently replaced.

Logout or owner change clears the visible target, receipts, history, filename and
blob link. Old async responses cannot repopulate a new session. Language changes
rerender this panel's original receipt, current claim, status and loaded-file label
without a payment write. English and Russian labels use the event timezone,
including the repeated hour at DST changes.
