# Durable order selection draft

## Implemented boundary

The store, owner agent API, session-authorized Mini App API, ru/en form and native
/orders entry implement the [selection plan](shuttle-orders-selection-plan.md).
Mixed draft/agent paths in that embedded
surface and unattended model behavior remain unaccepted.
This is not full
orders or meal-workflow acceptance.
The agreed owner-only ordering, evidence-time
capacity reservation and unchanged Python massage rules are recorded in the
[infrastructure priorities](shuttle-infrastructure-priorities.md).
They are not
implemented by this selection draft; remaining financial policies are separate.

A selection is an unsubmitted owner/event draft. Saving creates no recipient,
submitted order, capacity hold, debt, evidence or payment. A future submission
must independently check its agreed policies and current user intent.

Migration38 adds current drafts, immutable operation receipts and a Mini App action
journal.
It converts no existing data.
Down takes exclusive
locks on all three tables and refuses to remove any retained draft or history.

## Saving and recovery

Read the enabled orders catalog and your selection for an explicit event first.
A first unsaved selection has version0 and an empty array. Saving requires current
account binding, organization/bot, enabled orders module and an assigned pass;
unpaid/overdue qualifies, waitlist or administrative status alone does not.

Send expected_event_version, expected_catalog_version, expected_draft_version and
selections:[{offer_id,item_id,quantity}]. No client price, currency, total, recipient
or owner is accepted. Nonempty saves use complete server pricing and the database
observation instant in the same transaction as the draft and receipt. Current
versions must match; stale input is never silently repriced.

Each new accepted operation advances the draft revision. An explicit selections:[]
clears the draft and leaves a new revision with no quote. It cancels no order.
Missing/null arrays are invalid. Repeating the same operation and normalized body
returns its original result, even after a newer edit or clear. Selection pairs are
sorted before hashing; reordering an identical set is the same request. Duplicate pairs are invalid. Reusing an operation ID with changed normalized
arguments conflicts. Quantities reuse existing calculation bounds;
the complete serialized saved result is bounded to60KiB, with a64KiB jsonb storage
check. A caller must not sum partial quotes with shared per-offer charges.

Reads preserve saved quantities and the original quote/timezone/version provenance
when current items disappear or sales close. A historical quote is not a current
price guarantee. Fresh owner/event entitlement is still required before reading or
replaying any receipt. Owner operation IDs serialize across events; event then
owner locks protect snapshot/CAS writes. No second connection is opened inside the
write transaction.

## Agent tools and wire contract

This slice first added its methods at manifest mvp-38. zinusya_orders retains catalog and quote,
and adds:

- events: POST /internal/agent/orders/events with optional after cursor. It lists
  at most20 entitled events per page, including past events for draft history.
  Use next as after; listing does not select an event.
- selection: POST /internal/agent/orders/selection with event_id.
- save_selection: POST /internal/agent/orders/save_selection with the expected
  versions and explicit selections. The normal protocol1 envelope carries a
  nonzero UUID operation_id and identical idempotency_key. Confirmation mode is
  none for this non-submitted draft write; use only the user's requested quantities.
- selection_operation: POST /internal/agent/orders/selection_operation with the
  requested receipt operation_id in arguments. It returns the original result.

The write's status_path is /internal/agent/orders/save_selection/status. Real Loom
sends only protocol_version and operation_id, without arguments or event_id.
The store resolves that operation's event privately, releases its owner-only
lookup lock and rechecks event-to-owner authorization before disclosing the result.
This wire constraint is the exception to explicit event selection on normal reads
and saves; it grants no cross-event access.

A succeeded status confirms a stored draft, never submission or payment. A rejected
agent-write receipt leaves no draft/receipt changes. A missing or currently inaccessible receipt
is not_found and remains unknown to the caller; it does not authorize replacing
the operation ID. A503/unknown commit outcome must reconcile or retry the same
operation and exact body. The public browser has no general internal-tool proxy.

## Mini App HTTP routes and manual context

The dedicated routes require the existing ZinusyaSession bearer. They use the
existing origin/content-type/shutdown guards, bounded strict JSON decoding, IAM
check and current session/binding checks. Owner identity comes from the session.
No browser argument selects another owner or forwards arbitrary internal tools.

- GET /app/orders/events accepts optional after UUID and returns the same bounded
  events/next list as zinusya_orders.events. Assigned unpaid/overdue passes qualify.
  Past events stay listed while the owner's module/pass access remains available;
  a catalog need not exist yet.
- GET /app/orders/{event}/catalog returns the authorized current catalog and IANA
  timezone. GET /app/orders/{event}/selection returns the saved draft, including
  historical quantities whose sale has closed.
- POST /app/orders/{event}/quote accepts expected_event_version,
  expected_catalog_version and nonempty selections. It calculates without writing.
- POST /app/orders/{event}/selection accepts operation_id, expected_event_version,
  expected_catalog_version, expected_draft_version and explicit selections. The URL
  supplies event_id. This shares the agent's calculation, CAS and operation receipts.
- GET /app/orders/{event}/selection/operations/{operation} recovers the original
  owner/event receipt after current access checks. Missing or inaccessible records
  return404 while a valid session remains available; the result is still unknown.

A write serializes its operation with the same owner/operation advisory key as the
agent, then locks event, owner and session in that order. Session state, expiry,
key generation and current binding are checked after those lock waits. The draft,
receipt and manual-action journal commit in one transaction. Journal failure rolls
back all three. A replay returns its historical receipt without another revision
or journal row; replaying a prior agent operation cannot forge manual provenance.
A successful response is checked against the current session again before sending
its body. Reads also perform a final session check; expired/logout sessions suppress
the result instead of returning saved data. A final session failure can hide an
already committed save: reopen the same account and reconcile its original
operation. An HTTP401 alone does not prove that no save occurred.

Only fresh manual commits enter the next user-triggered prompt. Context contains
at most five completed actions before that task's creation instant, newest first,
for the same owner and currently accessible orders events. It includes operation
and event IDs, historical event/catalog/draft versions, clear flag and completion
time. It copies no quantities, prices, labels or session credentials. The agent
must read current selection/catalog to continue an explicit request. History is
not a current request, consent or authority to submit, reprice or repeat actions.
Saving through HTTP creates no Loom run or Telegram reply.

Invalid JSON/unknown owner/price fields or invalid versions return400. Stale CAS,
closed sales or missing complete pricing return409. Invalid/expired/logged-out
sessions return401; wrong POST origin returns403. Storage/commit uncertainty returns
503: reconcile the exact operation before another edit, and retain the same body
for a retry. The form implements this recovery as described below.

## Form behavior

The form explicitly selects an entitled event, pages the event list and shows
service times in the event's IANA zone with UTC offsets. Repeated DST wall times
remain distinguishable. Catalog labels use text nodes. Quantity controls retain
saved closed or removed items until the user explicitly clears them. Saved quote
provenance and the current server calculation are shown separately. Save selection
and Clear selection change only the draft; they do not place or cancel an order.

Changing quantities invalidates the displayed current quote. Unsaved edits block
event/page changes until save or explicit reload; changing interface language
preserves those edits. A catalog/event/draft conflict requires explicit reload
and review. The form never automatically reprices or overwrites a newer agent edit.

Before dispatching a save, the form persists its operation UUID, exact request and
verified owner in owner-keyed sessionStorage. No bearer is added to that record.
Storage failure prevents dispatch. Unknown results block new edits: reload checks
the original receipt, and Retry same save sends the original UUID and body. A
missing receipt does not authorize a replacement operation. A confirmed receipt
remains confirmed if the subsequent current-catalog read fails. The original
receipt and a newer current draft are displayed as separate facts.

Logout clears the rendered draft and in-memory account state. A pending operation
stays bound to its verified owner for recovery after that owner signs in again;
another account cannot recover it. This browser recovery uses sessionStorage,
so closing the tab or clearing browser storage can remove its local recovery data.
Immutable server receipts remain available through the authorized operation API.

## Native entry

The exact private-chat command /orders and its /menu keyboard entry use the normal
durable task, authorization and outbox without contacting Loom. The response opens
/app/#orders through a Telegram Web App button. It selects no event and changes no
quantities; the form checks the signed account session and current entitlement.
An unavailable Mini App origin produces localized retry guidance instead of a link.

The task option is now the shared MiniAppURL, derived from mini_app_origin even
when protected registration fields are disabled. RegistrationDetailsEnabled keeps
the protected-fields button separately gated; an origin alone cannot advertise
fields whose endpoints are absent. No new startup setting or migration is needed
for this entry. Existing details links still use /app/.

Opening /orders preserves an active agent question. Its bounded manual-navigation
fact enters the next explicit agent request, as history rather than instructions
or consent. Ordinary free-text answers continue the same agent run through the
merged steering contract. Opening the form is not submission, approval or payment.
