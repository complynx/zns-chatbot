# Meal editor acceptance scope

Next orders slice: the existing meal-ordering Web App, with customer details,
meal quantities and service charges, accessible from an owned Telegram order.

## Boundaries

- The bot serves the Mini App and validates Telegram `initData`: HMAC, timestamp,
  unambiguous parameters and user identity. Identity comes from the signed user,
  never an order URL or a submitted owner field.
- The bot delegates typed reads, quotes and edits to the existing authenticated
  Core API. Prices, availability, ownership, deadlines, versions and payment
  locks remain API rules. The web gateway has no Core database credentials.
- The browser edits names, current meal quantities and extras. Server quotes
  show dishes, packaging, utensils and total. Save uses the displayed order
  version and a request-bound retry key. Conflict preserves the user's draft
  until an explicit reload; it must not silently overwrite newer changes.
- A successful web edit updates the existing chat card. Agent extra changes
  preserve all meals and customer fields. API-owned history must describe meal
  changes as well as extras before this slice can be accepted.
- The fake Telegram opens the same editor from a Web App button and supplies
  signed fixture `initData`. It must exercise the same verification path; no
  debug user-ID authentication bypass. No real Telegram or production data.

## Required proof

- PostgreSQL integration: signed-data tampering/expiry/duplicates, foreign order,
  unknown user, Visitor writes, forged prices, canonical service quantities,
  stale versions, identical retries, deadline and paid/proof locks.
- Actual browser: open from chat, RU/EN menu, names and meal quantities, quote,
  save and reopen, old message refresh, agent continuation, separate mouse/touch.
- Concurrent chat edit versus open editor: stale save rejected, draft retained,
  explicit reload obtains current values. Failed transport can retry the exact
  save without duplicating the business operation.
- Both independent QA gates and existing booking/orders regressions.

## Implementation evidence

Gateway, editor, signed fake launches, shared chat cards and meal change history
are implemented. Browser mouse/touch checks passed quote/save, RU/EN, agent
continuation preserving meals, concurrent stale saves retaining draft and retry
after a committed response is lost. PostgreSQL integration proves canonical
service charges, owner isolation, version/payment locks and signed-data checks.
The full local quality gate passed: strict Go/JS lint and formatting, module
verification, build, PostgreSQL race suite, identity fuzzing, live container check
and three mouse/touch browser suites. Code QA found the combined customer name
was omitted; the gateway now derives it from first/middle/last fields as Python
does. Focused PostgreSQL/race and Go lint checks passed after that correction.
Fresh Code QA and independent Functional QA passed. The latter verified mouse
and emulated touch, persistence after bot/fake restart, signed-owner isolation,
stale drafts, lost-response retry and paid/proof locks. Its report is
`qa.local/functional-miniapp-pass1/report.md`. Deadline enforcement passed the
PostgreSQL integration test; independent black-box coverage needs a closed-event
or clock fixture and remains explicitly unverified.

Proof uploads, notifications, reminders and exports remain separate order
slices. This checkpoint does not establish full Python parity.
