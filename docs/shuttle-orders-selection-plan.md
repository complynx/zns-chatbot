# Next orders slice: durable selection draft

Status: selected engineering boundary after independent architecture and senior Go
consultations. Store, owner agent API, Mini App session API, ru/en form and manual context are implemented; see the
[current contract and evidence](shuttle-orders-selection.md). They are installed
on the owned runtime at schema38. [Four-path runtime QA](shuttle-orders-selection-qa-2026-10-16.md)
passes with actual Telegram/real IAM/Loom, direct form APIs and a manual provider.
Trusted WebView and unattended model acceptance remain open. Full order submission, the separate
meal workflow and massage bookings remain roadmap work. Pending D12/D13 questions
are retained in the ignored decisions file; this document does not answer them.

## Completed outcome to build

An entitled user can choose an event, select quantities, obtain an exact server
quote, save the selection, reload it after restart, edit through either Mini App
or agent and recover an uncertain save. Both interfaces share one owner/event
record. Manual saves become context only for the next user-triggered agent turn.

This is a real durable selection draft, bounded before submission. It creates no
submitted order, recipient, capacity reservation, debt, evidence or payment. The
owner is the editor of the selection; this does not impose a self-only recipient
policy on a future order. No automatic submission will occur when D12 is answered.
The future submission action must separately recheck the then-agreed policies and
obtain the required current intent. Saving these quantities is necessary groundwork
for the full order flow, not its replacement or acceptance.

The earlier D12 wording grouped cart work with unresolved order policies. This
engineering decomposition permits selection persistence without choosing those
policies. Recipient handling, capacity timing, proof-bearing edits, cash, partial
payments, cancellation/refund and actual menu/FX configuration remain pending.
Do not introduce a speculative configurable policy framework to hide those choices.

## Shared domain and storage

Reuse orders.Selection, normalized catalog data, integer calculation and existing
bounds. One current row per owner/event has a monotonic draft revision, referenced
event/catalog versions, canonical selections and the historical quote observed at
save. One immutable operation table binds the owner, operation UUID and normalized
exact request hash to its original result. A small form-action journal supplies
bounded redacted event/revision metadata to reactive context. No general workflow
or forms registry is required. Plan one additive migration after schema37 with
retained-data rollback guards; no existing catalog, identity or payment conversion.

A save provides event ID, expected event/catalog/draft versions and selections.
Prices, totals, currency, owner and recipient are never caller-authoritative.
A new accepted save advances the revision; exact operation/body replay returns
its original receipt without another revision. Reusing an ID with changed arguments
conflicts. Versions are retained after clear, preventing an empty-record ABA reset.
An explicit empty selection clears only this draft; it cancels no order. Its quote
is absent, rather than a fabricated zero-price quote requiring pricing policy.

Nonempty saves require a fresh complete quote for the explicit current versions
and database observation instant. Catalog changes require review, never silent
repricing and saving. Existing offer opening/closing and IANA/DST behavior apply.
Historical saved selections remain readable with current authorization even when
no new quote is available. Removed/closed items are not automatically deleted or
reduced. Show saved quote provenance separately from a current recalculation; neither
is a guarantee, reservation or payment. Clearing still checks entitlement and
expected versions. Bound the whole stored/result JSON and reject oversized results
without suggesting additive partial quotes, since shared charges are non-additive.

## Transactions, API and UI

Do not call OwnOrdersQuote from a write transaction: it calls OwnOrdersCatalog,
which opens another snapshot/connection. First extract the small transaction-local
catalog/entitlement snapshot seam and prove existing catalog/quote behavior unchanged.
The draft mutation then uses that seam on its own transaction connection. Follow
event-to-owner lock ordering; verify Mini App session/logout/revocation lock ordering
against the current implementations before introducing the wrapper. Recheck owner
binding, enabled orders module, assigned pass (unpaid/overdue qualifies), current
versions and session validity in the actual write transaction. A successful old
receipt is historical, but fresh access remains mandatory before disclosing it.

Expose owner read/save/operation-status methods in zinusya_orders and
dedicated session-authorized Mini App routes. Keep existing catalog and quote tools.
Loom status probes carry only operation_id: resolve its event privately and check
current access to that event before disclosing the historical receipt. Normal reads
and saves retain explicit event_id. Do not expose a general browser proxy into
internal/admin tools. Browser recovery
persists the operation ID and exact body before dispatch; reload reconciles the same
operation. Account switching cannot recover another owner's selection or receipt.

The simple ru/en form selects an explicit entitled event, renders dated offers in
its timezone, edits quantities, shows server quote provenance and saves/reloads or
clears the selection. Use clear labels such as Save selection; do not imply an
order was placed. Add deterministic Telegram entry independent of Loom. The shared
selection and form-action metadata must support continuation in both directions.
Trusted Telegram WebView acceptance remains the separate deferred CA gate; do not
claim that direct form API probes close it.

## Acceptance before advancing to submission

Complete the saved-selection workflow with form-only (Loom stopped), agent-only,
form-to-agent and agent-to-form paths. Check switching while the agent waits,
repeated saves/clear, reload and restart, lost replies before/after commit, original
historical receipt after newer edits, two-tab and agent/form CAS conflicts, changed
catalog/event versions, offer closing and DST, absent complete pricing, wrong owner,
foreign event, waitlist-only access, disabled module and revoked/expired sessions.
Prove zero submitted orders, reservations, passes or money effects and unchanged
old rows/receipts. Run focused behavioral checks, independent review loops, current
docs and real owned-stand QA. Manual provider proof is not unattended model proof.

The full /orders submit/proof/capacity lifecycle, separate /menu save/proof locks,
admin financial operations and massage bookings remain explicitly unfinished after
this draft slice. D12/D13 answers and their implementation gates still apply.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
