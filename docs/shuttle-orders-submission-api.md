# Owner order agent API component

The candidate exposes the reviewed order store through the `zinusya_orders`
module, now at source manifest revision `mvp-41` with the additive
[resource/demand contract](shuttle-orders-capacity-foundation.md).
This is the agent HTTP component of the
[submission plan](shuttle-orders-submission-plan.md), using the
[shared store](shuttle-orders-submission-store.md).
The new sold service/resource fields still have component-only acceptance.
Agent paths used real Loom with a manual provider.
Production is not activated.

## Routes and approval

Every route below is POST under `/internal/agent/orders/`. The caller supplies
its user bearer token; the API derives the owner and current deployment. No
owner/recipient, price or paid flag is accepted from request arguments.

- `submit/prepare` takes the usual read envelope with a nonzero operation UUID
  and event, expected event/catalog/draft versions. It returns the stored shared
  proposal, exact arguments, preconditions, expiry and a bounded description.
- `submit` requires the same operation/idempotency key and
  `confirmation.mode=exact_parameters` with the exact proposal UUID. The store
  rechecks authorization and freshness before a new effect, and recovers matching
  completed effects before freshness checks. The result contains `order` and
  the cleared next `selection` revision.
- `submit/status` takes only `protocol_version` and `operation_id`; it resolves
  the receipt's event privately and returns the same result shape as `submit`.
  A preparation alone returns `not_found` for this effect status.
- `cancel` takes event/order ID and exact expected order version. Its manifest
  mode is `none`: it still requires the user's explicit request, but has no money
  or capacity effect. The envelope requires matching operation/idempotency keys.
  A new cancellation needs submitted state and must precede the frozen cutoff;
  replay of a matching cancellation receipt remains possible after cutoff.
- `cancel/status` uses the same minimal recovery envelope and returns the same
  result shape as cancellation.
- `get` reads one current order by event/order ID; `list` reads a current page
  by event, optional after UUID and optional limit, default/maximum 20.
- `operation` reads a specific immutable `prepare`, `submit` or `cancel` phase
  by operation UUID. It returns phase metadata with its original result. Use
  `get` separately for current order state.

Status lookup is read-only. `not_found` is an unknown outcome, never proof that
execution did not happen or permission to replace an uncertain operation key.
Current identity, account, bot/organization, enabled module and assigned pass
remain mandatory for read, effect and receipt recovery. An administrative or
ambassador role alone cannot borrow an owner's pass. Disabled entitlement hides
retained status; account revocation denies the request.

The manifest declares effect, confirmation/preparation mode, idempotency,
deduplication window and status route. It describes canonical source-draft
identity as well as transport-key replay. Different transport IDs cannot create
a second order from a consumed revision. A new identical order requires an
explicit new draft revision. Submission confirms no money and reserves no
capacity; order proof/review is still a later component.

## Human-readable preparation and bounded results

The preparation summary follows the current explicit bot language, then Telegram
language, then the configured fallback. ru/be/uk use Russian; other languages
use English. It shows event, exact draft/event/catalog revisions, item and extra
charge line counts, exact decimal total and original sales cutoff in the event's
IANA timezone. Integer minor units are formatted without floating point. Event
metadata is quoted inside a dynamically sized code fence. Display language may
change on a preparation replay; the stored business proposal, expiry, quote and
operation receipts do not change.

Loom limits preparation descriptions to 4000 bytes. The summary stays bounded;
the agent must read `selection` and show its full composition and additional
charges before seeking approval. The summary does not replace that breakdown.
Native form review uses the same prepared snapshot; see
`shuttle-miniapp-order-submission-ui.md`.

Each `list` page is bounded both by count and by 60KiB of serialized result JSON,
leaving 4KiB of the manifest's 64KiB allowance for the protocol envelope. If the
next complete order does not fit, the page returns a cursor equal to the last
included order. Clients must follow `next` even when the page contains fewer
than the requested count. Snapshots are not truncated or reduced to summaries.
One oversized stored record fails safely instead of silently skipping history.
