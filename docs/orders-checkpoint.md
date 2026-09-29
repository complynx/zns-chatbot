# Orders checkpoint: extras and cash payments

Scope: authenticated order API, authoritative meal/extra calculations, Telegram
extra-service cards, shared manual/agent edits, cash payment requests and admin
acceptance/rejection. This checkpoint does not complete the Python migration.

## Implemented behavior

- Owner-scoped orders and admin-only payment inbox, paginated by stable creation
  order with item and byte limits. Client consumes every page.
- Server prices, event cutoff, capacity reconciliation, version and payment
  attempt checks, request-bound idempotency receipts.
- `/orders`: create, toggle extras, delete, request cash payment; Boris is the
  disposable payment-admin fixture. Payment decisions update Alice's old card.
- Agent proposals bind the same order version and executor. They cannot set the
  actor, edit another user's order, or perform human payment decisions.
- API-owned manual/agent history includes authoritative previous/current extra
  selections and totals in the action transaction, including direct API edits
  and system capacity removals. The model receives the last 30 owned changes
  and current owned orders. Failed actions and retries create no history entry.
- Fake Telegram displays user messages, bot messages and inline keyboards.
  Optional `compose.qa.yaml` exposes the sandbox API on loopback port 8091.

## Review evidence

- Code QA pass 1 found deadline handling for proof-country routing and unbounded
  list responses. Both fixed. Tests cover continuation after cutoff and a valid
  320-order response exceeding 1 MiB without blocking the next Telegram user.
- Code QA pass 2 found insufficient semantic manual history. Fixed with bounded
  extra-selection snapshots and explicit model-input assertions, including an
  added then removed transfer. The scripted fixture answers from that history.
- Fresh Code QA pass 3 found no substantive blocking findings; static review.
- Functional QA pass 2 found direct API edits missing from agent history.
  Fixed by moving authoritative snapshots into the API transaction and exposing
  owner-scoped history. Migration 004 preserves old audit rows without inventing
  missing selections. Functional QA pass 3 passed, including distinct direct-API
  removal recall and actual restart persistence with the same message ID.
- Code QA pass 4 found unbounded model context for large valid order collections.
  Fixed with compact summaries, full collection counts, explicit selection when
  multiple editable orders exist and authoritative full-order reload before an
  edit. A 320-order integration test also checks booking, ambiguity, old-order
  selection and preservation of meals/customer fields. Code QA pass 5 passed.
- Scale Functional QA exercised 76 orders / 270,893 response bytes: booking,
  selection, isolation, mouse/touch and field preservation passed. Its history
  check found the scripted fixture ignored user-requested agent removals. Fixed
  with manual/agent recall and explicit exclusion of system-origin changes.
  Focused Code QA passed. Fresh focused Functional QA passed six removal-origin
  mouse/touch cases and a 31-order / 89,881-byte collection with accurate recall,
  preserved manual/agent edits and unrelated booking. Evidence:
  `qa.local/functional-order-recall/`. This checkpoint is accepted; proceed to
  `meal-editor-checkpoint.md`.
- Functional QA pass 1 passed browser manual/agent ordering, payments, stale
  callbacks/attempts, permissions, concurrency, retries and restart persistence.
  Direct API checks proved forged prices are recomputed and paid edits rejected.
  Evidence: `qa.local/functional-orders-pass1/`. Later functional evidence:
  `qa.local/functional-orders-pass3/` and `qa.local/functional-orders-scale/`.
- Full local gate passed before the focused review fixes. After pagination and
  cutoff fixes, the full Linux PostgreSQL race suite passed. After history
  enrichment, Go lint and focused agent/integration tests passed.
- After the API history change, strict Go lint, focused HTTP/agent integration
  tests and the full Linux PostgreSQL race suite passed.
- After context compaction, strict Go/JS lint and formatting, full Linux race
  tests, and both mouse/touch browser suites passed. The final recall change
  passed Go lint and the agent suite.
- Staged diff remains empty; no commit, push or production change.

## Remaining order parity

Meal Web App/editor and customer details; proof attachments and payment-country
UI; proactive admin/customer notifications, capacity notices and two-day payment
reminders; XLSX exports, RU/EN content, old menu items and imported data. These
require their own integration/browser fixtures and both QA gates before order
parity can be claimed. Live OpenAI and real Telegram were not used in this gate.
