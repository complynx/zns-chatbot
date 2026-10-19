# Owner order agent API component

The candidate exposes the reviewed order store through the `zinusya_orders`
module, now at source manifest revision `mvp-41` with the additive
[resource/demand contract](shuttle-orders-capacity-foundation.md). The original
agent component gate below covered `mvp-40`. This is the agent HTTP component of the
[submission plan](shuttle-orders-submission-plan.md), using the
[shared store](shuttle-orders-submission-store.md). The [native transaction/context component](shuttle-miniapp-order-submission-store.md)
has passed its own gate. Native
[HTTP](shuttle-miniapp-order-submission-http.md) and
[UI](shuttle-miniapp-order-submission-ui.md) components also have separate gates;
actual owned-stand submitted-unpaid four-path acceptance has since passed, as
recorded in [the real QA report](shuttle-orders-submission-real-qa-2026-10-17.md).
The new sold service/resource fields still have component-only acceptance.
At this agent component gate, the owned main stand was at schema39, manifest `mvp-39` and image
`sha256:e6c429ba15abb5cc5782ddb066581cda6456a41f70be96e8e37db714aedd2170`.
The later owned schema40 activation, native-only, agent-only and both mixed
actual Telegram paths are in `shuttle-orders-submission-real-qa-2026-10-17.md`.
Agent paths used real Loom with a manual provider. Production is not activated.

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

## Verification and remaining acceptance

The first API run passed 29 roots/31 events with Linux/race and real PostgreSQL.
Independent correctness review then identified that twenty full snapshots could
exceed the manifest's 64KiB result budget. That passing evidence is retained in
`.cache/shuttle-work/orders-submission-api-v1-qa/`; it did not cover large pages.
The API now applies the byte budget and cursor rule above.

The final gate passed 30 roots/32 events, zero failures and zero skips, on real
PostgreSQL with the race detector. It includes the store/edge cases, new HTTP
approval/recovery/access/manifest/language checks, saved-selection HTTP recovery
and registration-details schema/access checks. Status responses are asserted
against the original effect response, including after cancellation. Invalid or
missing exact approval, mismatched idempotency, foreign ownership and changed
operation bodies are rejected. The list test creates 21 orders with forty long
bilingual item labels each, proves its unbounded page exceeds 64KiB, then follows
all byte-bounded HTTP pages and verifies all 21 complete snapshots exactly once.

A fresh independent textual review after the fix reported no actionable
findings. The separate quality reviewer also found no actionable issue. Initial
lint findings were corrected before the frozen final gate; the final scoped API,
DB and integration lint run reported zero issues. Host compilation passed before
the final gate. The final manifest froze 865 source inputs, unchanged across the
run. Evidence is in `.cache/shuttle-work/orders-submission-api-v2-qa/`.

The test container used a 2GiB memory limit without swap, 2CPU, Go memory limit
384MiB and two test cases in parallel. It exited zero without OOM. Both API QA
pairs and their dedicated PostgreSQL volumes were removed. All ten main service
IDs, images, start times and restart counts remained unchanged. No commit, push,
PR, publication or production deployment was performed; the Git index is empty.

This gate uses the HTTP test harness and mock identity peers. It is not real
Loom approval, real-model or actual Telegram acceptance. Native HTTP protections
and recovery now have a separate component gate documented in
`shuttle-miniapp-order-submission-http.md`; native form behavior has a component
gate in `shuttle-miniapp-order-submission-ui.md`. At that original gate, the four
real Telegram paths and restart recovery were still pending. The four paths
subsequently passed in `shuttle-orders-submission-real-qa-2026-10-17.md`; controlled
lost-response/clean bot restart passed in
`shuttle-orders-response-loss-real-qa-2026-10-17.md`. Hard-kill acceptance remains
required. Full order proof/capacity/payment/meal behavior and new-field live
acceptance remain later work. No production-readiness claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
