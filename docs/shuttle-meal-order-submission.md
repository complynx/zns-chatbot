# Meal order preparation and submission

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

## Submitted meal reads and operation recovery

The source now includes own submitted-meal detail and UUID-cursor pages, plus
operation lookup without trusting an event hint. Shared read helpers retain
service-order behavior and always select a fixed persisted business kind.
Current owner binding, assigned pass and matching enabled module remain required
on historical receipts. Administrative authority alone grants no owner access.

Native meal detail, list and operation reads validate the bearer/generation
inside the event-owner-session transaction.
Logout while a read waits for the
event lock denies the read.

Read get for current state and operation for the original result. Cancellation
must not rewrite the submission receipt. Original-key recovery survives opening
another database connection. Pages retain cancelled history and never cross
owner or service/meal boundaries.

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

The snapshot version gate now rejects stale draft event/catalog lineage with409.
Behavioral native/agent HTTP tests cover rejection, resaving and successful explicit
submission.
Genuine
invalid-term failures still follow the existing server-error path.
