# Meal menu: composition and durable drafts

The meal module provides typed catalogs, pure composition evaluation, durable
owner drafts and financial checkout.

[Financial checkout](shuttle-meal-order-submission.md#browser-checkout-source)
and [shared proof/review](shuttle-meal-payment-evidence.md) use the existing order
engine.
These bounded
checks do not establish autonomous model quality or exact native-preparation
adoption.
No new runtime dependency is required.
The
[frozen terms builder](shuttle-meal-terms.md) remains a pure component reused by
financial integration; calling that builder alone creates no order or payment.

## Catalog and composition

`MealCatalog` declares currency/scale and up to 14 named days. Each day has a
stable ID and at least one lunch or dinner service. Services use explicit start,
end and sales-window instants; normalization preserves instants in UTC with
microsecond precision. The owning event supplies the IANA display zone. A day ID
or weekday name never chooses a date. Each service has up to 100 named items with unique IDs, and the normalized catalog is bounded to 48 KiB.

Item and combo prices are final integer minor-unit prices, including mandatory
charges. This catalog version does not model extra charges. Lunch combos are
optional as a pair: when configured, the catalog must contain soup, main, side
and salad categories and both fixed prices. Uncategorized items remain usable
for individual lunch or dinner choices.

Supported lunch modes retain the intended Python behavior:

- `no-lunch`: an explicit complete choice, with no components or price.
- `individual-items`: a set of lunch item IDs. An empty set remains complete as
  in Python; a later final-action presentation must make that choice clear.
- `combo-no-soup`: one main, side and salad, priced once at the fixed combo price.
- `combo-with-soup`: those three components plus one soup, priced once at its
  own fixed price. Individual component prices are not charged again.

A missing lunch choice is incomplete whenever that day offers lunch. Dinner is
optional, including an empty selection. A dinner-only day requires no lunch
choice. Supplied IDs must belong to that day and meal; combo IDs must match their
category. Unknown modes, duplicates, irrelevant mode fields and foreign IDs fail.
The weak Python check for merely non-null combo indices is not copied.

`EvaluateMeals` returns copied canonical choices, missing fields, price lines and
`known_amount_minor`. Item sets are sorted and omitted days are represented in
catalog order. A partially chosen combo is retained with missing-component
information and contributes no amount. Only complete composition receives a
`quote`; the known partial amount must not be presented as a payable total.
Totals are bounded by the existing 9,000,000,000,000,000 minor-unit limit. Zero is
valid arithmetic; this does not decide whether a future zero-total order exists.

Evaluation is independent of wall-clock time and creates no sale permission,
capacity hold, order or payment. `MealService.SalesOpen(now)` exposes the inclusive
opening and exclusive closing boundary. Save transactions check versions and compare changed lunch/dinner choices with
persisted state, then check the affected sales windows using database time.
Unchanged closed services can remain while another open service changes.
Removal/clear is also a change.

## Durable catalog and draft store

Migration53 adds immutable event/catalog versions, administrative preparations
and receipts, owner/event drafts, immutable save receipts and a native-session
action journal. The independent event module is `meals`; it does not require
`orders`. An assigned unpaid pass is sufficient for the existing module-access
check. Current user, bot, organization, binding, revocation and native-session
generation checks still apply before historical operation replay.

Catalog administration uses exact prepared arguments, event/catalog compare-and-
swap versions and a five-minute preparation. Execution checks fresh authority
through the supplied callback, then checks database time against the original
expiry again. A slow authority check cannot extend consent. The administrative HTTP adapters enforce the current global IAM administrator
role before configuration, preparation, execution and status reads. Store methods
are not independently exposed endpoints.

Draft writes require explicit choices and expected event/catalog/draft versions.
An empty array clears the draft; omitted choices fail. Canonical request hashing
is independent of the latest catalog. After current access checks, an original
successful receipt is returned before stale-version or latest-catalog validation.
Old removed item IDs therefore do not prevent receipt recovery. Reusing an
operation ID with different arguments conflicts. Historical drafts and receipts
retain their saved evaluation, catalog version and event timezone without repricing.

Transactions lock event, owner and, for native saves, session in that order.
Native save, immutable receipt and action journal commit together. Failed journal
insertion rolls all three back; retrying the original operation can then succeed.
This store creates no service order, capacity allocation or payment entry.

Changed lunch and dinner services use the current authorized catalog's window at
database time. An administrator can reopen existing unsubmitted drafts by
extending the current deadline. When a service is removed, its source window is
the conservative removal guard. Clear checks every changed service and never
partially saves. Unchanged means equal canonical choices and the full normalized
service definition, including prices, names and item order. This deliberately
conservative rule means editing even an unselected item in a closed service can
block another draft edit. Administrators must retain that closed definition or
reopen its window. Currency or scale changes also count as changed terms.

## Agent HTTP contract

The static `zinusya_meals` module is independent of service orders. Its methods
are `catalog`, `draft`, `save_draft` and `draft_operation`, under
`/internal/agent/meals/`. Catalog accepts an explicit event and optional version
(omitted/zero means latest); draft reads are event-scoped. Save requires operation
UUID equal to the idempotency key, expected event/catalog/draft versions and an
explicit choices array. The server derives the authenticated owner. Optional
lunch/dinner fields preserve incomplete drafts and days offering dinner only.
Omitted or null lunch remains unchosen; explicit `no-lunch` is a complete opt-out.
Clear uses an empty choices array. Unknown owner fields fail validation.

`save_draft/status` accepts Loom's argument-free operation envelope. The explicit
`draft_operation` method accepts the target UUID in arguments. Both return the
same original receipt after current entitlement checks. A missing result is
`not_found`, an unknown outcome that does not authorize replacing an operation.
Expected business rejections use the terminal `rejected` envelope; storage failure
remains a retryable HTTP503. The manifest describes deduplication, historical
prices, incomplete totals, cutoff/version rules and absence of financial effects.

Administrator discovery includes `meal_catalog` and `set_meal_catalog`.
`/internal/agent/administration/set_meal_catalog/prepare` freezes exact normalized
arguments for five minutes. Execute requires its proposal ID and matching
operation/idempotency UUID. Current IAM authority is checked on every endpoint,
including status, and again before first publication. The status endpoint resolves
the current actor's original operation without arguments, privately finds its event
and rechecks the binding in event-to-owner order. Administrator status never grants
an assigned pass for owner catalog reads. Bounded schemas publish final prices,
explicit instants, ru/en labels and the supported composition fields.

The next run found that Huma forbids a nullable tag on a referenced object;
optional HTTP fields now accept omission/null without that tag.
Both are fixed with focused HTTP
coverage.
Failed lint runs are retained.
Successful-fixture
archives contain metadata only, not business backups.

## Native HTTP and reactive action context

The authenticated Mini App now exposes these paths:

- `GET /app/meals/events`, paginated by the optional UUID `after` cursor.
- `GET /app/meals/{event}/catalog`, with optional nonnegative `version` (zero or
  omitted means latest; positive selects retained history).
- `GET /app/meals/{event}/draft` and
  `GET /app/meals/{event}/draft/operations/{operation}`.
- `POST /app/meals/{event}/draft`, with operation UUID, expected event/catalog/
  draft versions and explicit choices. Empty choices clears only this draft.

The server derives the owner from its session. Strict bounded JSON rejects
unknown owner/price fields and trailing content; absent versions or choices fail.
Reads check current IAM and session before lookup, then recheck the current
session before returning data. Save rechecks bearer/generation within the shared
transaction after event locking, and checks the session again before disclosure.
An expired session cannot obtain catalog data after waiting behind a lock. If a
write committed before a final session rejection, the original operation remains
the recovery identity; a new session must reconcile it rather than replace it.

Meals event discovery includes assigned unpaid passes and entitled past events
without requiring service orders, an open window or a configured catalog. It
uses the existing 20-event UUID pagination shape. Orders and meals share only the
pagination/authentication HTTP transport; each retains its entitlement query and
error mapping. Current meals access also governs saved draft/receipt reads.

Native save, immutable receipt and journal remain one transaction. Before the
next explicit user-triggered agent task, context includes at most five native
completion records preceding that task's creation, newest first, filtered by
current owner/event entitlement. Metadata carries original operation/event and
version IDs, clear flag and completion time; it carries no session token, choices
or money. The prompt identifies it as untrusted history, not instructions,
consent or a new request, and directs the agent to `zinusya_meals.draft` and the
saved catalog version. Native changes start no agent task or unsolicited reply.

New roots cover native save/replay after harness service restart, agent/native
continuation, explicit clear and later reactive context, foreign owners, stale
versions, malformed bodies, logout, transaction rollback after journal failure,
session expiry behind an observed event lock, independent past-event discovery,
expiry behind catalog locking, and explicit catalog history/version validation.
The event-lock test observes waiters blocked by its own transaction backend PID,
so another parallel test cannot satisfy its synchronization condition.
This
synchronization gap was found and fixed during independent review.

SQLC compile succeeds and
regeneration is idempotent.
The API/database/task/integration linter reports
0 issues; its first failed duplicate-handler log is retained.
Shared event-list
transport replaces that duplication.
Its 861-byte
archive contains metadata only because successful fixtures remove their schemas.
The earlier compile-only command ran no tests and is not acceptance evidence.

Evidence uses actual isolated PostgreSQL and harness IAM/Telegram/Loom peers.
The harness stop/start is not an operating-system SIGTERM/process-restart proof.
At that gate the meal form was still pending; its component acceptance follows
below.
Real IAM/Loom, both form/agent handoffs through a normal bot restart and
real Telegram WebView acceptance remain open.

## Shipped meal form and native command

The embedded Mini App now includes `meals.js` and a separate ru/en meals section.
`/menu` opens `#meals` without Loom. The command still lists the other native
commands in its text, while its attached button opens meals; `/orders` continues
to open the separate service-order form. An existing agent question stays pending
across `/menu`, a Loom outage and harness restart. Test-only launch URL overrides
use the existing configuration; production needs only its ordinary public URL.

The form discovers entitled events and shows explicit event-zone service and
sales instants. It supports incomplete lunch/component choices, all four lunch
modes, optional dinner, dinner-only days and explicit clear. Individual items may
be an empty set, separately from `no-lunch`. Combo prices are displayed once;
components remain selected by category. Saved names, missing components and the
known partial amount or complete draft total come from the saved catalog and
server evaluation. A partial amount is explicitly not payable; even a complete
draft is neither an order nor a reservation or confirmed payment.

When a saved draft uses an older catalog, its editor is read-only and retains
that catalog's names and amounts. The explicit “Start a new draft with the current
menu” action opens an empty local composition under the current catalog. It sends
no write and leaves the saved draft untouched until an explicit save. This avoids
silently remapping removed items or repricing an old receipt. Version conflicts
and closed-window rejection preserve visible edits and require explicit reload
before another save. Clearing is a draft-only operation.

Before sending, the controller stores a bounded owner/event-bound operation and
body in session storage. An unknown outcome disables editing and offers lookup or
retry of that same request. Page recreation first recovers the original operation;
not_found does not create a replacement. Invalid or unavailable recovery storage
fails closed. Once a receipt is confirmed, failure to reload current state is
reported as confirmed save plus reload failure rather than uncertain payment or
another write. Owner/epoch guards ignore late callbacks from an old account.
Authentication denial releases the parent busy state before clearing the account.
Selecting the empty event option clears the old writable context. Native select
focus survives a mode change, and labels precede their fields. Labels use text
nodes rather than HTML, including event/menu data. Language changes update saved
content, controls and status without changing choices.

The CI browser-behavior command now includes 12 meal scenarios.

Separate headless Edge acceptance used the shipped meals section, CSS and script
in a real 390×844 DOM against an owned synthetic HTTP fixture.
It exercised
incomplete-to-complete combo/dinner, placeholder clearing, keyboard focus,
escaped hostile labels, ru/en presentation and committed-response loss followed
by page reload.
Three writes produced three receipts; reload issued no additional
POST and no page errors occurred.
This is
actual DOM proof, not Telegram WebView or real IAM/Loom/PostgreSQL browser QA.

Preserved observations include the first full-UI failure on an old massage-copy
assertion.
It reproduced with the pre-slice parent app; only that expectation was
updated, retaining cancellation/history checks.
Earlier logs and screenshots remain unchanged.

This remains source
acceptance at schema53.
A future financial bridge must freeze full composition
and sold names into approved order terms and reuse the existing evidence/payment
lifecycle.
Financial submission, import, capacity policy, real Telegram WebView
and production acceptance remain open.

Linux candidate and platform correction (2026-10-19)

The pinned temporary BuildKit builder was removed without
OOM.
This image is not
installed on the primary.
Agent-to-native continuation additionally crosses
a normal0.640s bot SIGTERM restart.
Four drafts
have six original receipts and three native journals, all tasks/runs/deliveries
finish, and financial tables remain empty.
Both QA databases and logs were
archived and the separate nine-service project removed.

## Agreed financial policy

Daniel chose no submitted order for an empty composition, one explicit payment
proof deadline for all meals at an event (the legacy Food deadline), and unlimited
meal quantities. Ambassador review may finish after a timely proof submission.
An empty selection is distinct from a selected item whose configured price is zero.

The source catalog now accepts optional `payment_deadline` as an explicit absolute
instant.
Normalization validates microsecond precision, copies the value and stores
UTC.
Existing descriptive catalogs may omit it; no deadline is inferred from their
service windows.
The administrative catalog API advertises the field in source
manifest mvp-87, and immutable meal terms retain the original configured deadline.
This adds no SQL migration and does not enable order submission or payment evidence
yet.
