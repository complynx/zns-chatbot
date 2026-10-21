# Meal menu: composition and durable drafts

The source now has a typed meal catalog and pure composition evaluator in
`internal/orders`. It supports the separate Python `/menu` meal choices without
changing existing service-order selections. This is a domain component, not a
finished meal-order workflow. Migration53 now adds versioned catalogs, durable
drafts and operation receipts. The agent HTTP API is implemented at manifest
`mvp-60`. Native HTTP APIs and reactive action context now share that store.
The installed ru/en meal form uses those draft APIs. The later working-source
[financial checkout](shuttle-meal-order-submission.md#browser-checkout-source)
adds explicit meal submission, cancellation and shared payment-proof handling;
that source is not installed on the primary. A
[bounded runtime check](shuttle-meal-financial-runtime-qa-2026-10-21.md) accepts
native checkout/recovery, Loom writes/consent, pending-consent restart and native
proof/ambassador confirmation with owner/module isolation. Full release and primary
installation remain open.
No new runtime dependency is required.
The next [frozen meal terms](shuttle-meal-terms.md) component is implemented and
verified separately; it preserves full selected names and per-service windows
for future financial integration without creating an order.

The active Python references are `Food._load_menu` in
`zns-chatbot/plugins/food.py`, `static/menu_2025_1.json`, `static/menu.mjs` and
`templates/menu.html`. Other static menu files are not assumed active. The
legacy file contains Friday/Saturday/Sunday services but no actual event dates,
currency declaration or combo prices. Those must be explicit in a Go catalog;
Python numeric constants 555/665 are not a conversion rule.

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

## Composition verification

All 20 `internal/orders` test roots pass on Windows and Linux/amd64 Go 1.27.2,
including seven new meal roots and existing quote/capacity tests. No skips were
counted. Tests cover incomplete-to-complete combo progression, all lunch modes,
optional dinner, invalid references/categories/mode fields, canonical sets,
amount overflow and explicit zero. The time case verifies both occurrences of
02:30 across the Europe/Berlin autumn transition and exclusive sales cutoffs.
The package linter reports 0 issues. Independent correctness and quality reviews
found no actionable issues; the final normalization change received fresh review.

The Linux check used one network-disabled container, read-only source/module
cache/root filesystem, a 768 MiB memory limit with no extra swap and two CPUs.
Its first run compiled but could not execute the test binary because `/tmp` was
mounted noexec. That failed log/checkpoint remains retained. A fresh container
with exec enabled only on its temporary filesystem passed the unchanged source.
Both containers were removed and neither was OOM-killed. The first lint command
also used Go 1.27.0 from PATH and did not check the package; the corrected command
used Go 1.27.2, found one complexity issue, and the simplified implementation then
passed. These failed observations were not rewritten as successful runs.

Ignored evidence: `.cache/shuttle-work/meal-foundation-v1-qa/`. Existing order
source files remain unchanged. The installed primary stays schema49 / `mvp-56`.
The last built schema52 / `mvp-59` candidate predates this source-only foundation;
it does not prove the new component's installation or runtime acceptance.

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

## Store verification and next step

The isolated PostgreSQL acceptance run passed all 18 selected integration roots
without skips: ten new meal roots and eight existing catalog/selection roots.
They cover incomplete/complete/clear/reload, original receipt recovery after
catalog changes, concurrent editors, current entitlement, native/owner mixed
writes, atomic journal rollback, independent lunch/dinner cutoffs, removed
services, administrative deadline extension, proposal expiry during authority
checking, retained-history rollback refusal and empty migration down/up.
SQLC compile passes and generation is idempotent. The database/integration linter
reports 0 issues. The final local unit run passed 20 orders roots and eight database
roots; 25 database roots skipped without a DSN and are not counted as PostgreSQL
acceptance. Independent final code reviews found no actionable findings. Review
caught the expiry-after-authority-check gap; the implementation and behavioral
proof now cover it.

The first PostgreSQL run passed all ten meal roots but failed one old API test
that pinned manifest `mvp-43` while source already published `mvp-59`. That test
now verifies a nonempty discovery revision while retaining its behavior checks.
The unchanged failure record remains; the fresh 18-root run passed. Each run used
its own PostgreSQL container/volume, 512 MiB memory limit with no extra swap and
two CPUs. Both were removed without OOM. The failed run retains a validated
387,053-byte database archive; the successful run's 861-byte archive is metadata
only because successful fixtures drop their schemas. It is not a business-data
backup. Both runs checked that primary data and application sources were
preserved; the second also checked unchanged integration-test sources.

Argument-free recovery now has a store entry point for Loom status probes. It
privately resolves the receipt's event under the authenticated owner, releases
that transaction and rechecks current entitlement in event-to-owner lock order
before returning the original result. A separate isolated PostgreSQL run passed
four receipt/history/access roots, including owner isolation, module removal and
restoration, and revocation. Its temporary container and volume were archived and
removed; the primary remained unchanged. Independent diff review found no
actionable findings. This adds no public endpoint or manifest method.

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

The accepted component run passed 16 selected PostgreSQL integration roots with
no skips, including three new HTTP roots for owner recovery/access, exact admin
approval/history/authority and incomplete/dinner-only composition. The full API
package passed all17 root tests without skips. The API/database/integration linter
reports 0 issues; fresh final code review found no actionable findings. This uses
real isolated PostgreSQL with the integration harness's IAM/Telegram/Loom peers;
it is not actual IAM/Loom runtime or browser acceptance.

Failures remain retained: the first run found a Huma name collision between the
domain and persisted `MealCatalog`; a distinct transport result type fixes it.
The next run found that Huma forbids a nullable tag on a referenced object;
optional HTTP fields now accept omission/null without that tag. Review also found
a shallow test copy that mutated the expected historical receipt, and the initial
required-field mismatch with the manifest. Both are fixed with focused HTTP
coverage. Failed lint runs are retained. All three isolated PostgreSQL containers
and volumes were archived and removed without OOM; accepted source/test hashes
and primary data remained unchanged during the final run. Successful-fixture
archives contain metadata only, not business backups. No image was built or
installed, and the primary remains schema49 / `mvp-56`.

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

The component gate passed all23 selected PostgreSQL integration roots without
skips: seven new native HTTP roots plus meal and neighboring order regressions.
New roots cover native save/replay after harness service restart, agent/native
continuation, explicit clear and later reactive context, foreign owners, stale
versions, malformed bodies, logout, transaction rollback after journal failure,
session expiry behind an observed event lock, independent past-event discovery,
expiry behind catalog locking, and explicit catalog history/version validation.
The event-lock test observes waiters blocked by its own transaction backend PID,
so another parallel test cannot satisfy its synchronization condition. This
synchronization gap was found and fixed during independent review.

All17 API and16 task unit roots pass without skips. SQLC compile succeeds and
regeneration is idempotent. The API/database/task/integration linter reports
0 issues; its first failed duplicate-handler log is retained. Shared event-list
transport replaces that duplication. Fresh final code review found no actionable
findings. The owned PostgreSQL container and volume were archived then removed
without OOM; primary data and application/test hashes were preserved. Its 861-byte
archive contains metadata only because successful fixtures remove their schemas.
The earlier compile-only command ran no tests and is not acceptance evidence.

Evidence uses actual isolated PostgreSQL and harness IAM/Telegram/Loom peers.
The harness stop/start is not an operating-system SIGTERM/process-restart proof.
At that gate the meal form was still pending; its component acceptance follows
below. Real IAM/Loom, both form/agent handoffs through a normal bot restart and
real Telegram WebView acceptance remain open. No new image was
built or installed; primary remains schema49 / `mvp-56`.

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

The CI browser-behavior command now includes 12 meal scenarios. All190 UI tests
pass without skips, including existing order, massage, review and parent-app
behavior. All36 selected PostgreSQL integration roots pass, covering the native
command family, current meal HTTP/agent contracts and mixed harness continuation.
The full API and task packages pass all17 and16 roots without skips; lint is clean.
Fresh final code review found no actionable findings.

Separate headless Edge acceptance used the shipped meals section, CSS and script
in a real 390×844 DOM against an owned synthetic HTTP fixture. It exercised
incomplete-to-complete combo/dinner, placeholder clearing, keyboard focus,
escaped hostile labels, ru/en presentation and committed-response loss followed
by page reload. Three writes produced three receipts; reload issued no additional
POST and no page errors occurred. Inspected screenshots show both 02:30 service
instants with GMT+2 and GMT+1 across the Berlin autumn clock change. This is
actual DOM proof, not Telegram WebView or real IAM/Loom/PostgreSQL browser QA.

Preserved observations include the first full-UI failure on an old massage-copy
assertion. It reproduced with the pre-slice parent app; only that expectation was
updated, retaining cancellation/history checks. The first PostgreSQL run stopped
on a neighboring massage test that still required `/menu`'s former reply keyboard;
the route-specific expectation now checks its meals form button. Review found and
fixed busy release, empty-event context and focus issues with targeted tests. The
first browser fixture omitted UTF-8 declarations and misdecoded Russian; the
production page already declares UTF-8. A fresh fixture retained that failure and
passed, then screenshots prompted a final labels/status adjustment and successful
rerun. Earlier logs and screenshots remain unchanged.

Both PostgreSQL containers and volumes were archived and removed without OOM.
The final source/test hashes and primary data were preserved. Headless browser
and local HTTP fixture were closed. That component gate built no image. A later
Linux candidate is now built and remains uninstalled, as recorded below. The
subsequent runtime gate below covers an owned stand with actual
IAM/Loom: native-only while Loom is stopped, agent-only and both handoffs. Restart recovery is demonstrated during the
agent-to-native handoff.  Financial submission remains separate.

Ignored evidence is `.cache/shuttle-work/meal-store-v1-qa/` and
`.cache/shuttle-work/meal-operation-v1-qa/` and
`.cache/shuttle-work/meal-agent-api-v1-qa/` and
`.cache/shuttle-work/meal-native-http-v1-qa/` and
`.cache/shuttle-work/meal-form-v1-qa/`. This remains source
acceptance at schema53. The primary remains
schema49 / `mvp-56`, and the earlier schema52 / `mvp-59` candidate excludes meal
code. Actual runtime acceptance now passes native-only, agent-only and both handoffs
on a separate stand; restart recovery is demonstrated during agent-to-native
continuation. See the runtime gate below. A future financial bridge must freeze full composition
and sold names into approved order terms and reuse the existing evidence/payment
lifecycle. Financial submission, import, capacity policy, real Telegram WebView
and production acceptance remain open.

Linux candidate and platform correction (2026-10-19)

The first Linux image build failed because `meal_draft_windows.go` was implicitly
Windows-only. The file is now `meal_draft_sales.go`, with identical contents; all
other release bytes match the accepted form source. Go file selection includes
the renamed file on both platforms. Fresh Linux unit verification passes61 roots
(db8, orders20, API17, task16). Another25 database tests skip without a DSN; these
are not PostgreSQL acceptance. The earlier36-root PostgreSQL gate remains the
component evidence. A first Linux unit attempt exhausted its1GiB container;
its failure is retained. A serial2GiB rerun passes without OOM and is removed.
Independent review of the byte-preserving rename has no actionable findings.

The unchanged release Dockerfile now builds a Linux/amd64 candidate,
`sha256:232e1d477ea5685990e1622e2ad41dac07f460e58a53c8f9a552bf99d44d4a0d`,
at schema53 / `mvp-60`. The pinned temporary BuildKit builder was removed without
OOM. The source freeze and primary preservation checks pass. This image is not
installed on the primary. Ignored evidence is `meal-linux-portability-v1-qa/`
and `meal-images-v2-qa/` under `.cache/shuttle-work/`; the failed first build
remains in `meal-images-v1-qa/`. [Actual IAM/Loom runtime acceptance](shuttle-meal-runtime-qa-2026-10-19.md)
now passes all four paths. Agent-to-native continuation additionally crosses
a normal0.640s bot SIGTERM restart. Four drafts
have six original receipts and three native journals, all tasks/runs/deliveries
finish, and financial tables remain empty. Both QA databases and logs were
archived and the separate nine-service project removed. The provider was
controlled, so unattended model behavior remains outside this gate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk


## Agreed financial policy, 2026-10-21

Daniel chose no submitted order for an empty composition, one explicit payment
proof deadline for all meals at an event (the legacy Food deadline), and unlimited
meal quantities. Ambassador review may finish after a timely proof submission.
An empty selection is distinct from a selected item whose configured price is zero.

The source catalog now accepts optional `payment_deadline` as an explicit absolute
instant. Normalization validates microsecond precision, copies the value and stores
UTC. Existing descriptive catalogs may omit it; no deadline is inferred from their
service windows. The administrative catalog API advertises the field in source
manifest mvp-87, and immutable meal terms retain the original configured deadline.
This adds no SQL migration and does not enable order submission or payment evidence
yet. Schema remains68; the installed primary still runs mvp-86.
