# Durable order selection draft

## Implemented boundary

The store, owner agent API, session-authorized Mini App API, ru/en form and native
/orders entry implement the [selection plan](shuttle-orders-selection-plan.md).
They were first installed on the owned stand at schema38; later components
advanced the stand schema. [Runtime acceptance](shuttle-orders-selection-qa-2026-10-16.md)
passes for actual Telegram ingress/delivery, real IAM/Loom and direct form APIs,
with a manual provider and locally signed form proofs. The later
[embedded launch check](shuttle-miniapp-local-launch-qa-2026-10-16.md) verifies
native draft writes in Telegram Web. Mixed draft/agent paths in that embedded
surface and unattended model behavior remain unaccepted. This is not full
orders or meal-workflow acceptance. The agreed owner-only ordering, evidence-time
capacity reservation and unchanged Python massage rules are recorded in the
[infrastructure priorities](shuttle-infrastructure-priorities.md). They are not
implemented by this selection draft; remaining financial policies are separate.

A selection is an unsubmitted owner/event draft. Saving creates no recipient,
submitted order, capacity hold, debt, evidence or payment. A future submission
must independently check its agreed policies and current user intent.

Migration38 adds current drafts, immutable operation receipts and a Mini App action
journal. It converts no existing data. Down takes exclusive
locks on all three tables and refuses to remove any retained draft or history.
No commit, push, production deployment or main-stand migration is part of this step.

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

## Verification boundary

The transaction-local catalog extraction passed identical before/after10-root,
31-pass-event Linux/race real-PostgreSQL gates. Independent source review and scoped
lint passed. The first store gate retained a failed test fixture with nanosecond
closing time; catalog rejection was correct. The fixture was corrected to explicit
microsecond precision. Its failure remains in the ignored orders-selection-store-qa
report; it was not relabelled.

The corrected store gate passed15roots/36events, no failures/skips/OOM, and retained
all ten main-service fingerprints. It covers new-store recovery, canonical replay,
old receipt after clear, changed-operation conflict, no revision reset, competing
CAS writers, concurrent identical replay, wrong binding/owner, disabled/revoked
access, catalog edits/closed sales, and rollback retention. Its exact QA containers
and database volume were removed. Evidence:
.cache/shuttle-work/orders-selection-store-v2-qa/checkpoint.json.

The API's initial gate passed19roots/40events with real PostgreSQL and mock peers,
including the exact argument-free Loom status request, explicit receipt lookup,
caller-owner rejection, idempotency-key mismatch and manifest write declarations.
The final post-format/manifest gate passed21roots/42events across integration,
orders and API packages, with no failures/skips/OOM. All818frozen inputs and ten
main-service fingerprints stayed unchanged. Exact temporary containers and volume
were removed with terminal0/noOOM. Evidence:
.cache/shuttle-work/orders-selection-api-final-qa/checkpoint.json. Fresh independent complete source
reviews found no actionable findings. Scoped db/api/integration lint reports zero
issues. These are focused developer checks, not real IAM/Loom/browser acceptance
or a new full-suite result. At that API component checkpoint the owned runtime remained at schema37 with
its earlier image; no new Telegram interaction was performed in that slice.


The Mini App backend passed30roots/51events under Linux/race with real PostgreSQL
and mock peers,823frozen inputs unchanged, no failures/skips/OOM. Existing catalog,
quote, owner tools and protected/profile form paths remain in that focused gate.
New paths cover HTTP quote and save, restart/exact replay, agent-to-form continuation,
historical receipts after later edits, explicit clear, no proactive run, redacted
reactive context, access disablement, wrong owner, malformed input/origin/logout,
atomic journal failure/retry, expiry after an event-lock wait, expiry during a
blocked catalog read, agent/form CAS, replay provenance and past-event parity.
Evidence: .cache/shuttle-work/orders-miniapp-final-qa/checkpoint.json.

The event-list route registration was subsequently moved beside catalog reads to
avoid a boilerplate dupl lint match, with no handler or contract change. The three
affected route/manifest cases passed3roots/3events on the final source, with all
823inputs and ten main-service fingerprints unchanged, no failures/skips/OOM and
exact temporary containers/volume removed. Evidence:
.cache/shuttle-work/orders-miniapp-routes-qa/checkpoint.json. Independent backend,
quality and complete source reviews found no actionable findings. Scoped api/db/task/
integration lint reports zero issues. This is component HTTP acceptance with mock
IAM/Loom/Telegram; no real stand migration or trusted WebView check occurred.

The actual ru/en form adds13 focused Node VM behavior tests; the existing30
Mini App tests also pass on the final source, with all eight recorded inputs
unchanged. Cases include owner/account isolation, persistence before dispatch,
unknown saves before/after commit, same-body replay, terminal version conflicts,
removed/closed items, preserved unsaved edits, delayed old-owner reads, event
identity and auth-controller integration. Evidence:
.cache/shuttle-work/orders-ui-node-final-qa/checkpoint.json.

The final focused Linux/race gate passed31roots/52events with real PostgreSQL and
mock peers. It includes the earlier backend matrix and the bounded static-asset
route for orders.js. All824 release/test inputs and ten main-service fingerprints
stayed unchanged, with no failures/skips/OOM. Evidence:
.cache/shuttle-work/orders-ui-wire-qa/checkpoint.json. Exact temporary containers
and volume were removed after terminal0/noOOM. This is not a full-suite rerun.

Independent UI review passes found three event-identity defects: an old receipt
survived an event switch, a recovered event outside the first page had no visible
selector entry, and selecting the placeholder could hide the editor's target.
All were verified and fixed; the final complete v3 source review found no
actionable findings. The placeholder failure was also reproduced in the browser
and retained under .cache/shuttle-work/orders-ui-browser-qa/review-failure.json.

Actual browser acceptance then passed against a loopback SDK/API fixture serving
exact final JS/CSS. It covered the explicit event, distinct DST offsets, text-only
labels, server quote/save, committed lost-response reload without another POST,
uncommitted lost-response reload followed by identical explicit retry, clear and
logout. Five POST attempts produced four receipts; the retry reused the operation
and body. The fixture process and agent-created tab were removed. Evidence:
.cache/shuttle-work/orders-ui-browser-v2-qa/accepted.json. The screenshot was shown
inline; file export returned EPERM, so no saved screenshot artifact is claimed.

This browser fixture is not real Telegram initData, IAM, Loom or trusted WebView
acceptance. At that UI checkpoint the owned stand remained at schema37. The
later native-entry, image and runtime checks are recorded below and in the
[runtime report](shuttle-orders-selection-qa-2026-10-16.md).
No Telegram messages, production change, commit or push occurred in this UI slice.

The native-entry final focused gate passed53roots/74events under Linux/race with
real PostgreSQL and mock peers, all825 release/test inputs and ten main-service
fingerprints unchanged, no failures/skips/OOM. It includes the orders matrix,
existing structured navigation/name/registration/detail paths, /orders without
Loom or protected fields, localized active-question continuation across restart,
and unavailable-form guidance. The command package compiled with no matching test
roots; this does not claim its unrelated tests ran. Evidence:
.cache/shuttle-work/orders-native-final-qa/checkpoint.json. Exact temporary Go/PG
containers and volume were removed after terminal0/noOOM. Scoped cmd/task/db/
integration lint reports zero issues. A source review identified loss of the
protected-fields availability gate; it was fixed and the existing unavailable-form
case now also configures the generic origin. Fresh complete v2 review is clean.
That native-entry component checkpoint preceded actual Telegram delivery and
schema38 installation. The later runtime report records those accepted steps.

The unmodified release Dockerfile built image
sha256:3b447e5b97f4f7fa72b51b9131b38cfccb5f8e6f1ad7b0227987dbbae588320f. All721 release input paths and bytes match the
accepted native-entry source; the bounded2GiB/2CPU builder was removed without OOM.
Evidence: .cache/shuttle-work/orders-images-qa/checkpoint.json. Harness review
closed a deleted-input loophole by comparing the complete accepted release path
set as well as hashes; the already-built image's recorded inputs were revalidated.
Fresh complete harness review is clean. This is a local image, not publication.

That exact image migrated an isolated dump of the owned schema37 to38. All
75 existing business-table hashes were preserved; the three new draft, receipt
and manual-journal tables were empty. The migrator and snapshot PostgreSQL exited
successfully and were removed, with no persistent clone volume and all ten main
service fingerprints unchanged. Evidence:
.cache/shuttle-work/orders-schema-qa/checkpoint.json. This verifies migration of
the copy, not application activation or downgrade of retained selection data.

An initial source review questioned the avatar-notice allowlist in
Dockerfile.dockerignore. The actual image contained all three required notice
files with matching source hashes; see orders-schema-qa/avatar-license-image-check.json.
A subsequent [context canary check](shuttle-docker-context-qa-2026-10-16.md)
resolved the discrepancy: the ancestor !docs/ rule included unrelated documentation,
including those notices. The source policy now admits only the required source,
migration and license subtrees. All719 required payload files match source bytes;
three synthetic exclusion canaries stay out. No Go source or running image changed
in this config-only step. Owned activation and four-path runtime acceptance pass
under the limits in the linked report. Production remains unchanged.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
