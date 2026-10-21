# Meal order preparation and submission

Status: installed on the owned primary at mvp-89/schema69 after Daniel's
October21 policy answers. [Full CI and installation](shuttle-meal-primary-qa-2026-10-21.md)
pass; bounded [browser checkout](shuttle-meal-final-browser-qa-2026-10-21.md),
[native/runtime proof review](shuttle-meal-financial-runtime-qa-2026-10-21.md)
and [controlled-agent payment](shuttle-meal-agent-payment-qa-2026-10-21.md)
have separate acceptance scopes. The chronological source checkpoints below
retain their original test counts and failures; none substitutes for real
Telegram WebView or exact native-preparation adoption acceptance.

## Business and storage boundaries

Meals share the existing order/preparation/operation tables with service orders,
but retain order_kind=meal. Order/preparation reads and writes explicitly supply
the business kind. Receipt lookup spans kinds so comparison can reject operation
IDs belonging to another kind. Existing service callers supply service. There is no second
payment engine and no new schema migration.

PrepareOwnMealOrderSubmission requires current owner/bot/organization binding,
an assigned pass and the independent meals module. The orders toggle grants no
meal access and meals grants no service access. Receipt replays also pass current
entitlement checks. Operation namespace and event-then-owner locks are shared
with service-order operations, including attempts to reuse the same UUID.

Preparation loads the current catalog/draft at exact event/catalog/draft versions
and freezes complete composition. Empty composition returns ErrEmptyMealOrder
without an order or draft consumption. A selected zero-price dish is not empty.
An explicit common payment_deadline is required; absent legacy configuration is
not inferred from any service. Selected services must currently be on sale, and
the database clock must precede the common deadline. Preparation expiry is the
earlier of the usual five-minute window and the common deadline.

Capacity is explicitly unlimited: the frozen version1 capacity snapshot has an
empty demand list. A nil capacity policy retains its existing unknown meaning
for service orders. No hold or payment is created during meal submission.

## Exact terms and recovery

The meal terms retain all selected names, composition and original charge lines.
The shared monetary quote has one basket line with the same total; it does not
repeat those descriptions. Future human approval views must show the meal terms,
not treat the basket caption as a full composition. This keeps valid large menus
within the existing60KiB JSON /64KiB JSONB receipt budgets without dropping detail.

Submission checks the original proposal/hash/expiry and recalculates current
terms, ignoring only observation time in the exact comparison. Canonical identity
is owner/event/kind/source-draft-version. A distinct native/agent request for an
already submitted draft recovers the original order and cannot consume a newer
draft. A successful meal result has meal_draft and no service selection; service
JSON keeps its existing selection object. Atomic persistence stores the order,
consumes its preparation, clears/increments only the meal draft and saves the
original receipt. Connection reopen and operation-status reads use those receipts.

OwnMealOrder and OwnMealOrderOperation check current meals access and scope reads
by the persisted kind. API callers also enforce fresh IAM/native-session checks.

## Native session boundary

PrepareMiniAppMealOrderSubmission, SubmitMiniAppMealOrder and
CancelMiniAppMealOrder validate the native bearer inside the business transaction,
including receipt replay. They use the same event-owner-session lock order and
operation namespace as service orders, but authorize the independent meals module.
The caller must check current IAM before entry. Native and agent methods share
exact preparations, canonical orders and immutable receipts; opening another
connection does not change their identity. CancelOwnMealOrder provides the agent
side of the same cancellation. Cancellation retains original terms and receipts
and uses the persisted meal kind at the shared update boundary.

The original backend stage added store methods only. The later transport and
browser stages described below expose them with explicit consent.
Shared payment-evidence/review source work is tracked in the follow-up above.

## Backend verification

Initial native PostgreSQL behavior selection passed before the boundary case.
Independent review reproduced duplicate descriptive data overflowing a receipt
for a valid100-item menu. A new failing case established the defect; the quote now
retains one monetary basket line and full details remain in frozen meal terms.
The first Linux race attempt failed before tests because the local Go image had
no gcc; an owned compiler image was built. A later expiry test fixture was fixed
to preserve expires_at > created_at while moving both timestamps into the past.
Those original failures remain retained. The final isolated Linux Go1.27.2 /
PostgreSQL race run passed17 root tests /21 cases, including8 meal roots, with
no skips or OOM. Configured lint passed with0 issues. Two independent reviewers
checked the first implementation; after the receipt-size fix, a fresh final
review found no actionable findings. No full CI, runtime installation or complete
meal checkout acceptance is claimed.

The subsequent native-store stage passed57 root tests /81 cases under Linux
Go1.27.2, PostgreSQL and the race detector, including3 new native-meal roots.
It covers session expiry/logout/generation and foreign-owner/organization denial,
logout during event-lock waiting, cross-channel receipt recovery, cancellation,
and existing service-order/payment/review paths. No tests skipped; lint passed
with0 issues and an independent code review found no actionable findings.

That stage's first broader run exceeded the test database's512MiB cgroup: tmpfs
storage plus buffers caused a PostgreSQL child to be OOM-killed while the container
itself stayed alive. The failed log and memory.events counters are retained.
Recreating only the synthetic database on an owned disk volume with32MiB shared
buffers completed the same frozen-source test selection with zero OOM counters.
This infrastructure correction is not an application recovery acceptance test.

## Submitted meal reads and operation recovery

The source now includes own submitted-meal detail and UUID-cursor pages, plus
operation lookup without trusting an event hint. Shared read helpers retain
service-order behavior and always select a fixed persisted business kind.
Current owner binding, assigned pass and matching enabled module remain required
on historical receipts. Administrative authority alone grants no owner access.

Native meal detail, list and operation reads validate the bearer/generation
inside the event-owner-session transaction. Logout while a read waits for the
event lock denies the read. API callers must still check current IAM and delivery
access; these store methods alone do not establish HTTP or browser acceptance.

Read get for current state and operation for the original result. Cancellation
must not rewrite the submission receipt. Original-key recovery survives opening
another database connection. Pages retain cancelled history and never cross
owner or service/meal boundaries.

The frozen read-stage source passes29 Linux/PostgreSQL race roots /41 cases,
including the new owner-isolation and lock-wait logout cases and existing service
order behavior. No failures, skips or OOM remain in the accepted run; lint reports0
issues and fresh independent source review is clean. An earlier fixture assigned
a second leader into a one-place test event and correctly received409; the fixture
now uses the free follower place. The failed log is retained. The owned read-stage
runner, PostgreSQL volume and network are removed. Evidence is under
.cache/shuttle-work/meal-financial-flow-v1/checkout-v1/.
This is source/backend acceptance, not real-IAM/Loom or complete checkout QA.

## Agent and native financial transport

Working mvp-89 source registers prepare, submit, cancel, get, list, operation and
write-status tools in zinusya_meals. Native routes under /app/meals/{event}/
provide submit/prepare, submit, cancel, submitted order detail/pages and immutable
operation-receipts/{kind}/{operation}. Module bindings are fixed when registering
routes: request bodies cannot select service instead of meal authorization.
Native writes require an allowed origin, an authenticated current session and
explicit confirmed=true on submission. Store transactions recheck session access.

Preparation summaries are bounded for Loom transport. The bot's dedicated meal
submission consent view reads the exact stored preparation and shows its full
frozen composition, prices, total and event-local deadline. Longer consent text
uses multiple Telegram messages with the keyboard on the final chunk. Presentation
does not submit the order, confirm money or consume a newer draft.

The next explicit user request includes earlier native financial receipt IDs,
operation kinds and persisted order_kind, limited to five currently accessible
records. Kind selects zinusya_orders or zinusya_meals for subsequent reads.
Historical metadata never grants consent or starts proactive agent work.

Independent static review of this transport diff found no actionable findings.
The transport passes 46 roots /62 cases across four bounded Linux/PostgreSQL race
batches, with no failures, skips or OOM. Configured lint reports0 issues, and
DB/API/task package tests pass. The owned runner, database, volume and network
are removed. No complete browser checkout, real IAM/Loom transport acceptance
or primary installation is claimed.

## Browser checkout source

The meal form now shows the exact stored preparation before enabling submission:
selected components, combo or individual charge lines, total, confirmation expiry
and the common event-local payment-proof/cancellation deadline. Combo components
are descriptive; their individual menu prices are not charged a second time.
Only an explicit confirmation sends the submit request. Current orders include
cancelled history, explicit cancellation and a link to the shared proof form.

Before prepare, submit or cancel, sessionStorage retains the exact owner/event,
operation kind and request body. A lost response blocks replacement writes.
Reload reads the original operation receipt; an unavailable receipt leaves the
original request for explicit retry. Recovering a preparation displays it for
review and does not submit it. A submission receipt is historical: the form
reloads the current draft and orders separately and does not clear a newer draft.
Changing events clears old order cards and pagination before any awaited read,
including when the next event fails to load.

The proof controller reads meal details through /app/meals/{event}/submitted
and retains the meal kind in its owner-bound pending record. Evidence, file and
receipt operations use the existing shared /app/orders/{event} payment routes,
whose store authorization selects the persisted business kind. Existing service
pending records keep their original shape. Meal pending-proof text says awaiting
review without claiming a capacity hold; only an ambassador confirms payment.

The controller/mock-API UI selection passes365 cases with no failures or skips.
It covers explicit review/submit/cancel, meal proof routing and lost-response
recovery alongside existing modules. The original failing event-switch test is
retained. Fresh independent source review found no actionable findings. These tests use a simulated
DOM, not a real browser, Telegram, IAM or Loom. No additional runtime resource was
created for them. Source evidence: .cache/shuttle-work/meal-financial-flow-v1/browser-v1/.

The subsequent [bounded runtime check](shuttle-meal-financial-runtime-qa-2026-10-21.md)
accepts native checkout and receipt recovery in actual Edge with real IAM/DB and
this QA stand's Loom stopped. Real Loom read tools also see the native order and
original receipt through a scripted provider. Normal idle SIGTERM/restart preserves
financial tables. The later check also proves actual Loom writes with plain-text
consent and pending-consent restart, native proof/ambassador review and runtime
owner/module isolation. A real cleared-draft UI bug was corrected and independently
reviewed; the resulting UI suite passes366 cases. Both databases/logs are archived
and the nine-service stand removed. Full release and installation remained open
at that checkpoint; their subsequent acceptance is linked above.

Combined release review then found that normal event/menu changes could turn a
stale draft into a503 during preparation, retaining an uncertain native request.
The snapshot version gate now rejects stale draft event/catalog lineage with409.
Behavioral native/agent HTTP tests cover rejection, resaving and successful explicit
submission. The UI conflict/reload path is covered too;367 UI cases pass. Genuine
invalid-term failures still follow the existing server-error path. See the runtime
report for the interrupted first release attempt and its corrected replacement.

Private source snapshots, logs, ownership/resource records and review evidence:
.cache/shuttle-work/meal-financial-flow-v1/. The isolated database and test runner
used bounded memory/CPU and did not use the main stand or Loom developer stands.
Earlier accepted stages' runners, databases and isolated networks are removed.
The transport stage used its own bounded database and has also retired it. The primary
runtime and domain readback remain unchanged at schema68 / mvp-86.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
