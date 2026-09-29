# Order payment instructions and language: acceptance scope

After XLSX acceptance, port the payment-options screen from
`zns-chatbot/plugins/orders.py:handle_cq_pay` and the cash contact message.
The existing proof/cash state machine is already separate from this read-only UI.

- The owner can open payment instructions for a specific order. Display the
  current server total in BYN and RUB, configured transfer instructions, and
  available Belarus administrators with their regions and contact information.
- Keep payment instructions in event configuration. Sandbox uses clearly fake
  recipients; never copy real payment details into its seed or tests.
- A cash request identifies the chosen administrator and explains that the seat
  is reserved only after acceptance. Opening instructions must not create or
  confirm a payment, replace an attempt, or unlock a paid order.
- Expose a user-authorized API contract. Foreign orders and users without booking
  access cannot fetch another person's payment context. Telegram and agent
  requests use the same path and the requesting user's rights.
- Agent may open the payment GUI; it does not claim money was transferred.
  Manual edits and agent continuation must refresh an already opened instruction
  message when the order total or payment state changes.
- A stale button must use current authoritative data. Closed ordering and paid
  states must not invite a new payment or show active unavailable actions.
- Contacts are typed Telegram links. Extend the fake Telegram UI to exercise
  link buttons safely without sending real messages.

## Accepted slice

Owner-authenticated `GET /v1/order-events/{event}/orders/{order}/payment-instructions`
returns a single database snapshot. It contains current totals, payment eligibility,
localized event instructions and available administrator contacts. Telegram uses
an editable payment message; its contact links are exercised locally in the fake UI.

`GET /v1/me/preferences` and `PUT /v1/me/preferences/language` manage the owner's
language. `/language` exposes en/ru buttons; `/language en` and `/language ru` also
work. Initial Telegram language is retained as a canonical requested tag so a future
catalog can satisfy it. Explicit user choices win over later Telegram defaults.
Locale fallback is exact/base, explicitly compatible language, then English:
be/by and uk/ua use Russian when needed; Polish uses English.

The new language and payment UI is localized. Other existing Telegram screens and
Mini App text still need migration. This slice does not claim complete bot i18n.
The registry supports future catalogs; count formatting uses CLDR and money uses
integer minor units. See `i18n-design.md` for extension and date-format boundaries.

Focused testify/PostgreSQL tests currently pass for owner isolation, read-only
payment views, deadlines, revocation, content fallback, language initialization,
and editing the same payment message after a locale change. Browser mouse/touch
coverage is added in `platform/tests/language-payments.mjs`.

The full local quality gate passed on 2026-09-25: pinned Go/JS lint and format,
module verification, vet/build, real PostgreSQL race tests, fuzzing, live checks
and eight browser suites. Vulnerability scanning found no reachable/imported
vulnerabilities; one module-only advisory remains outside imported code.

Code QA pass 1 found a language mismatch when retiring a deleted payment card;
the localized retirement and focused PostgreSQL test fixed it. Fresh pass 2
returned no actionable findings. A separate fresh Code QA also accepted the
FQA-requested `language_code` transport fixture; its unit test and lint passed.

Independent Functional Senior QA passed mouse/emulated-touch language controls,
initial fallback and persistence, owner isolation, contacts, current totals and
same-message refresh, cash/paid/deleted states, stale callbacks, deadlines and
revocation. Evidence: `qa.local/functional-language-payments-pass1/report.md`.
The reviewer restored fixtures and released the stand. Paid synthetic evidence
`PQ6JFTYNELOH3ZGWL2BN24BW3X` remains intentionally preserved.

Limits: Functional QA did not reproduce literal duplicate-update-ID/crash or
concurrent preference-write cases; focused PostgreSQL tests cover the retry
boundary. Touch was emulated. The scripted model has limited English vocabulary;
the actual agent payment path was exercised with a supported request. Full bot
i18n and semantic profile/media intake are separate stages, not accepted here.
