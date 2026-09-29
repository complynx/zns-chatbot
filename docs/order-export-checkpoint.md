# Order XLSX export: acceptance scope

Source:
`zns-chatbot/plugins/orders.py`, `handle_cq_xlsx` and the active
`/exportfoodorders` command. Full bot parity is not claimed by this checkpoint.

- Export the selected event's non-deleted orders through an authenticated Core
  endpoint. Only event payment administrators with current access may export.
  The bot must use the requesting user's identity, including agent requests.
- Preserve five sheets: `Заказы`, `Содержимое`, `Итоги`, `Посуда`, `Активности`.
  Include identifiers, customer, timestamps, payment routing, payment and
  validation flags, BYN/RUB totals, catalog dish/service counts and six extras.
- Preserve Python's reporting semantics: proof and paid orders count in paid
  aggregates; pending cash and unpaid orders do not. Administrator validation is
  a separate flag. RUB is BYN multiplied by 30.
- Use saved order prices for aggregates. Retired dishes remain in detail rows;
  only current catalog items appear in catalog aggregate columns. Do not
  reprice historical orders with the current menu.
- Take one consistent database snapshot for the whole report. Stable row and
  column ordering; no silent pagination truncation. Explicit size limits must
  produce a useful error, never an incomplete workbook presented as complete.
- Customer and historical catalog text must remain literal text, never formulas
  or executable links. Include Unicode and spreadsheet-formula-like fixtures.
- Deliver a downloadable `orders.xlsx` through the Telegram command and an
  administrator button. Agent export requests use the same authorized path;
  workbook contents do not enter model context. Record the export in the
  requesting user's interaction history without exposing other users' data.
- Preserve usable headers, frozen header rows, filters and bounded column widths.
  No production data or real Telegram delivery in the sandbox.

Proof: testify tests read the resulting workbook and check exact cells and
aggregates across unpaid/cash/proof/paid/deleted states, historical prices,
unknown items, event and role isolation, and safe text. Browser acceptance must
download the actual Telegram attachment with mouse and touch, including denied
manual/agent attempts. Fresh Code QA and independent Functional Senior QA are
required before advancing.

Implementation and verification:

- API export uses an event-scoped repeatable-read snapshot and pinned Excelize.
  Telegram command, button and typed agent proposal share user authorization.
- Local full quality gate passed, including the new sixth browser suite and
  pinned `govulncheck`. Runtime dependency fixes leave no reachable known
  vulnerabilities; an advisory for unused `x/crypto/openpgp` remains module-only.
- Code QA pass 4 is clean. Review fixes reject text beyond Excel's UTF-16 cell
  limit and preserve valid U+FFFD text; both have workbook round-trip tests.
- Functional pass 1 independently parsed six mouse/touch downloads, checked
  payment aggregates, permissions, concurrent edit/export consistency and all
  rows beyond the API page boundary. Evidence:
  `qa.local/functional-export-pass1/report.md`.
- Requested sandbox controls now support historical saved choices, administrator
  revocation and isolated oversized batches with guarded cleanup. Fresh
  affected Functional QA passed these controls and historical exports:
  `qa.local/functional-export-edges-final/report.md`.

Acceptance passed both independent QA gates and the local quality gate. Historical
unknown dishes/services appear in details, not current catalog aggregates.
Activities retain the six legacy-supported keys; arbitrary extra keys are not
part of the legacy export contract.
