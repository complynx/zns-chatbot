# Shared order identity: service and meal

Migration54 adds explicit business identity to shared order storage. `OrderKind`
is `service` or `meal`; `OrderSource` remains the separate transport identity
(agent, Mini App or Telegram). This is an expansion for future meal submission,
not an enabled meal financial workflow. Source schema is54; the agent contract
stays `mvp-60`. The primary remains schema49 / `mvp-56` and is not changed.

## Storage and service boundary

Orders, preparations and operation receipts have a non-null `order_kind`, with
`service` as the compatibility default. Existing rows gain that metadata without
rewriting JSON snapshots, operation results or request/business hashes. Current
SQL writers explicitly insert `service`; old binaries that omit the column also
remain service writers. This compatibility window assumes no meal writer is
enabled while old service-only binaries are running.

Canonical uniqueness is now `(user_id,event_id,order_kind,source_draft_version)`.
Thus independent service and meal draft version1 can coexist. Pending-proposal
lookup also distinguishes the kind. Composite foreign keys bind order to
preparation and operation receipt to its preparation/order with the same owner,
event and kind. Existing operation primary keys stay unchanged. Collision reads
inspect all kinds, so a UUID used by a meal operation cannot be reused as a
service operation; disclosure/recovery returns only service receipts.

All currently exposed order APIs remain service APIs: preparation, canonical
lookup, consumption, owner get/list and cancellation explicitly filter service.
Both service record decoders reject another kind before JSON decoding. This
prevents unsupported meal JSON from becoming a zero-valued service snapshot.
No business-kind argument or field was added to the public service result JSON.

Evidence is deliberately still service-only. Its new kind column has both a
service default and `CHECK(order_kind='service')`; a composite foreign key binds
it to the same owner's service order/event. Even a direct SQL insert cannot
attach service evidence to a meal order. Existing reviewer, capacity and notice
paths therefore keep their existing service semantics. No meal catalog/resource
policy is borrowed from generic orders, and no meal submission handler exists.

## Migration and rollback

The expansion is transactional and locks the affected tables before changing
identity constraints. Service-only Down/Up preserves historical snapshots,
receipts, hashes and evidence. Down refuses any retained meal order,
preparation or operation history before dropping columns/constraints. It does
not erase or convert that history. Run rollback only with the compatible old
application; the new binary's queries require schema54. No production migration,
primary installation, commit or push was performed in this stage.

## Verification and preserved observations

Three new PostgreSQL behavioral roots prove service snapshot/receipt/evidence
preservation across Down/Up and replay; independent canonical identities,
service proposal selection, owner reads, cancellation rejection and UUID
collision/recovery isolation; and owner/event/kind foreign keys plus failed
premature meal evidence through both application and SQL paths. Synthetic meal
rows test isolation only; they are not evidence of a working meal checkout.

The broad124-root orders/native/evidence run produced122 passes and two failures.
One was a preexisting `mvp-43` global revision assertion (manifest source bytes
were unchanged and already `mvp-60` before this stage); only that global assertion
was replaced with a nonempty check, retaining exact order recovery/idempotency/
approval assertions. The other was a new proof fixture using timestamp precision
not accepted by attachment intake; it now uses the established evidence helper.
A fresh four-root probe reran all three new roots plus the corrected manifest
contract: all four pass without skips. Coverage reconciliation confirms only
those two test functions changed, all other test functions/files and every
release byte match the broad run. Together the retained observations cover all
124 selected roots with passing evidence; this is not a claim that the failed
broad command itself passed or that a second broad rerun was performed.

The earlier three-root probe had two passes and a failed FK assertion that
violated several constraints at once. The corrected check inserts a fresh,
noncolliding identity with an unused preparation, isolating each intended FK.
All failed logs and schemas/dumps remain retained.

Windows and Linux/amd64 Go1.27.2 each pass65 unit roots (db12, orders20, API17,
task16). Another25 database tests skip without a DSN and are not PG acceptance.
Final db/integration lint reports0 issues. SQLC compile and repeat generation
are successful and idempotent. Independent correctness/quality review and a fresh
final review have no actionable findings.

The owned PostgreSQL and SDK containers are removed. Both failed and corrected
PG databases were archived and validated with `pg_restore --list`. During cleanup
of the broad failed run, its PostgreSQL exited137 after a20-second stop request,
without OOM; its validated archive was already retained. This is a failed test-PG
shutdown observation, not bot graceful-restart acceptance. The corrected small
probe used a45-second cleanup allowance and exited0. No SDK container was OOM-killed.
Primary runtime/data preservation checks pass. Ignored evidence is
`.cache/shuttle-work/order-kind-v1-qa/`.

The existing uninstalled schema53 meal runtime image predates both frozen terms
and this storage expansion. No new image or actual IAM/Loom runtime acceptance
is claimed here. Next is a meal-specific exact preparation/submission adapter
using retained `MealTerms`, followed by explicitly authorized kind-aware evidence
and capacity integration. Zero-order, shared cutoff and meal capacity decisions
remain in ignored D30; no defaults were invented. The release hold remains active.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
