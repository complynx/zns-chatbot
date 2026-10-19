# Owner order payment proof form

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Source schema 42 / manifest mvp-43. This component adds an owner form and safe
attachment downloads to the [native owner evidence API](shuttle-miniapp-order-evidence-http.md).
It adds no database migration or module revision. The owned main stand remains
schema 40 / manifest mvp-40; this component has not been activated there.

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

## Evidence and limits

The isolated Linux/PostgreSQL race gate passed 75 top-level tests / 140 pass events:
62 integration roots / 94 events and 13 API roots / 46 events. No failures, skips
or OOM occurred; all 916 frozen inputs matched before and after that run. Scoped
Go lint passed with zero issues. Its exact containers and volume were removed;
the ten main services were unchanged. Gate artifacts are in the ignored
.cache/shuttle-work/order-proof-ui-v1-qa/ directory.

Two later JavaScript fixes translate retained results after language changes and
allow zero quoted totals. Go/SQL/migration inputs still match the race snapshot;
the JS-only delta is recorded rather than claiming the final JS ran in that gate.
The final four Node UI suites passed 86/86 tests, including actual app/controller
wiring, lost-response reload, exact retry, blocked storage, ownership changes,
history pagination, locale changes and zero-total recovery. Artifacts:
.cache/shuttle-work/order-proof-ui-node-v7/.

Independent source reviews found a missing history file-read control and rejected
zero-total quotes. Both were fixed. The final fresh v4-diff review found no
substantive actionable issues. The browser found the retained-result locale bug;
its fix passed focused behavioral tests and a repeat manual check.

Manual browser QA used the real product HTML/JS/CSS with a synthetic Telegram SDK
and API. It verified selection, required file loading, 25,68 -> 2568, original and
current states, history file reading with disabled submission, EN/RU switch,
reload discovery and logout cleanup. The fixture recorded exactly one payment
POST. DST display distinguished 02:30 GMT+2 from 02:30 GMT+1. The final reload,
history, file and logout checks used the final source. Zero-total recovery was
checked by Node, not by this manual fixture. Artifacts and screenshot:
.cache/shuttle-work/order-proof-browser-v3-qa/. The fixture and QA tab were closed.

These are synthetic browser tests, not a real Telegram/IAM/Go-HTTP end-to-end proof.
Separately, the existing schema-40 Mini App was reopened inside the real Telegram
chat: sign-in, event selection and reading an existing order worked after the
browser prompt was approved. That does not prove this new form on real Telegram.
[Native reviewer reads](shuttle-miniapp-order-review-reads.md) are now available
in source, alongside [reviewer writes](shuttle-miniapp-order-review-writes.md). Actual
mobile WebView downloads and real activation remain pending. The
[reviewer UI](shuttle-miniapp-order-review-form.md),
[reviewer context](shuttle-miniapp-order-review-context.md) and
[owner proof context](shuttle-miniapp-order-proof-context.md) now have separate
source verification. Notice delivery, owned image/migration gates and full
native/agent/mixed/restart acceptance remain pending. No production readiness or publication is claimed.

Earlier Node harness/assertion failures and the manual locale failure remain in
the ignored v1/v2/v5 Node and v2 browser artifact directories. They are not rewritten
as successful acceptance. Later accepted snapshots are separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
