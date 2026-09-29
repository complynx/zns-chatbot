# Script API parity: source inventory and next slice

Source inspection: 2026-09-26. This is an implementation plan, not independent QA or acceptance. No product or protocol changes are made by this report.

## Initial inventory contract (before implementation)

At the initial inventory, the bot registered six workflow/order operations plus eight memory operations, or twelve total when booking is disabled. It does not expose all Core APIs. `tools.$list()` and `tools.orders.get.$help()` already work; native JavaScript handles JSON transformations. Do not add jq, a second expression language, a virtual filesystem or a general HTTP client.

| Concern | Verified source anchor | Consequence |
| --- | --- | --- |
| Registry and live visibility | `platform/internal/bot/script_tools.go:43`, `:83` | Booking permission removes select/change; memory remains owner scoped. |
| Memory tools | `platform/internal/bot/memory_tools.go:55` | Summary/index/search/read/history/revision/sources/write are present. Write only handles private documents, not the complete knowledge command API. |
| Bindings and discovery | `platform/internal/scriptworker/execute.go:108`, `:129`; `platform/internal/bot/script_tools.go:287` | Bindings come from the initial visible descriptor snapshot; list/help reauthorize live. No pagination parameters are accepted by the current list binding. |
| Admission and dispatch | `platform/internal/bot/script_tools.go:235`, `:324` | Reauthorize, strictly decode/prepare, reserve, execute, store outcome. Keep this single boundary. |
| Durable receipts | `platform/internal/bot/script_tools_store.go:50`, `:79` | Actor/update lock, eight admitted calls, host-generated key, bound command before effects. Interrupted admission is uncertain, never proof of rollback. |
| API transport | `platform/internal/bot/client.go:29`, `:41`; `platform/internal/api/api.go:111` | Existing APIClient supplies authenticated user transport; Core verifies identity/user existence and domain rules. No script-controlled credentials. |
| Confirmation | `platform/internal/core/core.go:144`, `:162`; `platform/internal/orders/service.go:264` | Origin and action restrictions remain domain enforced. A user's manual capability does not grant the script a manual origin. |

## Domain coverage and reuse map

Core route registration at `platform/internal/api/api.go:75` already composes domain services. The existing typed APIClient is the reusable transport surface, not a generated replacement for domain rules.

| Surface still outside scripts | Existing reusable implementation | Principal constraints |
| --- | --- | --- |
| Own language preferences and pass profile | `bot/preferences_client.go:10`; `bot/pass_profiles_client.go:10`; API `preferences.go:11`, `pass_profiles.go:13` | Own reads first; language writes have `SetLanguageWithOperation` and require host operation keys. |
| Full orders, quotes, payment instructions/inbox/proofs/history/export | `bot/orders_client.go:11`, `:34`, `:56`, `:72`, `:79`; API `orders.go:12`, `export.go:13`, `proofs.go:14` | Current script summaries omit full choices. `orderPages` at `bot/orders_client.go:76` collects all pages; reuse page-level Core reads for bounded script paging, not unbounded aggregation. Admin/payment operations need distinct visibility. |
| Pass booking/invitations/queues/payment/admin/takeover/batches/export | `bot/pass_booking_client.go:11`; `pass_payment_client.go:22`; `pass_admin_client.go:12`; `pass_takeover_client.go:12`; API `pass_batch.go:11`, `pass_export.go:13` | Core `passbooking/capabilities.go:21` computes event actions through actual authorization. Targets, event, current versions and confirmation cannot be supplied as trusted authority by JS. |
| Massage parties/providers/slots/bookings/timetable/preferences/actions | `bot/massage_client.go:12`; API `massage.go:12` | Customer/provider/admin views differ. Add domain capability evidence before exposing restricted descriptors; reuse command executor for writes. |
| Shared knowledge moderation, legacy memos and pending proposals | `bot/knowledge_client.go:16`, `:48`, `:63`, `:75`; API `knowledge.go:18` | Preserve classification, independent human review and author/reviewer separation. Existing memory.read/search does not expose pending proposals. |
| Owner history and media | `bot/history_client.go:18`; `bot/media_client.go:15`; API `history.go:12`, `media.go:13` | Owner-private history is already paginated. Binary downloads/uploads need existing attachment handles and delivery flow rather than base64 JSON expansion. |
| Model settings, administrative utilities and administrative messages | API `model_settings.go:13`, `admin_utilities.go:14`, `admin_message.go:19` | Separate superuser/grant checks and message preview/confirmation; user authorization to message remains required. Never infer from `can_book`. |
| Host-only archive, source attachment, classification, notifications, Telegram metadata | API `memory_archive.go:26`, `memory_provenance.go:24`, `memory_assessment.go:17`, `notifications.go:19`, `telegram_metadata.go:12` | Service-scoped operational APIs are not user tools. They remain internal even for a normal authenticated script. |

The row paths above are relative to `platform/internal/` unless explicitly prefixed. This inventory is based on current source; older broad migration checklists are not proof of current absence or acceptance. `docs/memory-hierarchy.md` documents the implemented pagination, provenance and deletion contract and remains the memory source of truth.

## Smallest reusable extension

Use one host registry entry per public operation with descriptor, visibility predicate, typed prepare/execute adapters and response budget. The same authorized registry supplies binding names, list/help and dispatch. Move the current entries without changing their semantics, then add new entries using existing typed APIClient methods. Avoid three independent name switches and avoid reflection over every exported Go method. Reflection cannot distinguish internal service APIs, user data, confirmation, replay or safe result projections.

A small shared adapter may implement strict JSON decoding, empty arguments and typed result marshaling. Domain adapters still bind versions, event context, target grounding and durable command types; these are security-relevant translation, not duplicate business logic. Do not invent a generic `api.call({method,path,body})` escape hatch: it defeats undiscoverability, host-bound command fields and service audience separation. Add a new client method only when the Core endpoint genuinely has no reusable typed client.

The existing receipt union holds workflow, orders and knowledge commands. Add a typed receipt member only for each newly supported mutation domain, using the same reservation path. Reads can reuse the outcome-only record. Do not replay a callback from JS code or expand a failed run into a second automatic execution. Preserve refreshed agent state after writes, and invalidate affected cached evidence before the ordinary planning loop continues.

## Ranked implementation slices

1. **Registry reuse plus two owner reads.** Preserve all fourteen operations, list/help shape and restrictions. Add `preferences.get` and `profile.get` through `APIClient.Preferences` and `PassProfile`, both strict empty arguments and bounded typed results. This reaches sixteen descriptors for bookable users and delivers visible data access without changing the protocol limit or mutation contract. Stop after permission-safe discovery, actual script reads and receipt behavior pass. This is a useful next slice, not all-domain parity.
2. **Catalog scaling and bounded navigation.** Separate lightweight binding names from full help schemas; retain the existing ergonomic method calls. For the finite current domains, a bounded names snapshot plus lazy per-tool help is simpler than dynamic proxies. Add a paginated/filterable list only if measured name/summary size needs it. When pagination is introduced, worker `$list(options)` and host strict decoding must change together, with opaque continuation bound to caller and filter. Grant changes require restart/refresh semantics; a refreshed list must never advertise new names that the current VM cannot invoke. Keep call-time ACL checks even when an old binding remains in memory after revocation. Validate a catalog with more than sixteen real entries and worst-case schema bytes before choosing new limits.
3. **Owner reads across orders, passes, massage, history and knowledge.** Reuse existing endpoint pagination and small result projections; add missing byte-bounded domain pages where needed. Expose only current authorized operations, not names followed by predictable 403 responses. User-visible acceptance: discover a domain, get help, read it, follow all pages, then return a compact answer in both locales.
4. **Existing agent-authorized mutations across domains.** Reuse the ordinary bot command preparers where applicable (orders: `bot/order_agent.go:152`; pass execution: `bot/pass_agent_execute.go:16`) without importing their UI reply side effects. Add typed receipts, fresh resource validation and state refresh. Manual-only actions remain a confirmation handoff; parity includes the handoff, not a script bypass.
5. **Restricted/admin and media operations.** Add current Core-backed per-operation visibility, real attachment references, bounded exports and the same explicit human approval flows. Admin rights alone cannot expose host service-token routes. Complete a route-to-tool/confirmation/internal-only matrix before calling all-domain parity done.

## Budgets and failure constraints

- `scriptprotocol/protocol.go:16`: sixteen descriptors, thirty-two discoveries, eight business calls; 128 KiB arguments, 64 KiB per protocol result, 512 KiB traffic per direction. Catalog serialization is capped at 64 KiB; each input/result schema at 8 KiB; names at three segments. Increasing only descriptor count can still fail the byte budget.
- `scriptworker/worker.go:18`: worker source 16 KiB, input 128 KiB, request envelope 256 KiB, output 64 KiB. `agent/script.go:11` narrows model code to 4 KiB and returned model JSON to 4 KiB, with explicit input up to 128 KiB.
- `bot/script_tools.go:271` limits ordinary results to 2 KiB; `bot/memory_tools.go:15` allows memory 32 KiB. Memory domain pages are 24 KiB serialized, not merely a fixed item count. JSON escaping must be counted before output admission. An oversize response after a mutation is uncertain to the caller; it does not undo the mutation.
- Existing memory search/index pages can be empty and incomplete; continuation is mandatory. Current/revision reads are chunked. Eight business calls cannot traverse arbitrary complete histories; return compact evidence and explicit incompleteness, then continue through the existing bounded planning/run mechanism.
- Discovery uses a separate thirty-two-call allowance; do not consume business receipts for metadata. A large catalog needs bounded discovery without forcing thirty-two help calls into every script.
- No new JSON library is needed. Keep Sobek native array/object/JSON computation and the existing captured strict output serializer. Schema metadata is help, not authoritative input validation; typed decoders and domain validators still enforce execution.
- Preserve deletion scrubbing of retained script outcomes, host-observed current references before memory update/delete, historical/current distinction and trusted provenance attachment. Reusing memory result projection for unrelated domains requires a deliberate evidence-retention decision, not a blanket omission.

## Acceptance scope for the next slice

Fresh Code QA checks registry parity, strict arguments, privacy, receipt uncertainty and result bounds. Separate Functional Senior QA uses the Telegram-like stand: in en/ru discover and invoke the two owner reads, verify another user's data stays inaccessible, revoke booking between discovery and call, catch an unavailable call, and show existing memory continuation and mutation confirmation still work. Check direct Core/domain refusal as well as descriptor absence; HTTP-only checks are insufficient. No production publication, commit or push is implied by this plan.

## Implemented first slice (2026-09-26)

The first ranked slice is implemented, awaiting fresh independent acceptance. `platform/internal/bot/script_registry.go` now owns the shared authenticated descriptor/dispatch/result-budget entries; existing workflow/order and memory preparation/execution retain their domain logic. `script_profile.go` adds `preferences.get` and `profile.get` through existing typed owner-scoped clients. The catalog is now sixteen tools, fourteen when booking is disabled. Profile results allow 8 KiB for escaped valid personal fields; the final model result remains capped at 4 KiB.

Focused existing script and memory-script tests plus the new real PostgreSQL profile test passed with two parallel tests and one package at a time. New coverage checks en/ru preference values, strict owner-override rejection, private profile isolation, descriptor discovery/help, valid JSON escaping above 2 KiB and replay without reexecution. These tests exercise host callbacks and Core, not a full rendered Telegram acceptance flow.

Next concrete implementation is ranked slice 2: separate lightweight authorized binding names from full help metadata, preserve native `tools.domain.method()` and `$help()`, and scale catalog count/bytes under explicit bounded discovery. Do not continue adding domains behind the sixteen-descriptor ceiling or call this all-domain parity. Cursor/filter support, if needed after size measurement, must be implemented in both worker and host with current-ACL filtering and explicit binding refresh semantics.

## Implemented catalog and owner-read slice (2026-09-26)

The accepted sixteen-tool baseline measured 4562 bytes of full metadata and 673
bytes of binding descriptors. The expanded nineteen-tool catalog measures 5527
and 794 bytes respectively. The bot now sends name-only descriptors; existing
worker `$help()` already calls the current authorized host registry, so no new
RPC method, JS engine or arbitrary HTTP surface was needed. The fixed count cap
is 128, with the unchanged 64 KiB catalog and 512 KiB stream budgets. A test builds
128 maximum-length legal names below 32 KiB, rejects the 129th, and proves that
full metadata still cannot exceed the catalog byte budget. The VM binds and
invokes the last of 128 name-only methods. The application currently exposes
nineteen actual operations, seventeen for callers without booking permission.

New `history.page`, `orders.page` and `orders.read` reuse current authenticated
Core reads. History preserves archive sanitization and paginates at serialized
32 KiB as well as twenty events; orders navigation reads one Core page and emits
only IDs, versions, states and totals. Full-order JSON uses 6000-rune string
chunks, current owner checks and a version/snapshot hash bound cursor. Invalid
cross-owner/operation/resource continuations fail. Full JSON reconstruction uses
native JavaScript string concatenation and JSON.parse. Large traversals still
respect eight calls and the final 4 KiB model result; incompleteness is explicit.
Successful intermediate payloads use the same model-evidence omission pattern as
memory, while durable owner-private receipts and failures remain intact.

This implements bounded access to owner history and full orders, not all-domain
parity. Next implement scoped read catalog entries for pass events/current
booking/invitations and massage navigation using existing typed clients and
current per-domain authorization. Before restricted domain metadata is exposed,
reuse or add Core capability evidence for descriptor filtering. Keep service-only
APIs hidden and preserve manual confirmation. Current nineteen-tool list metadata
fits the existing unpaginated discovery response; further catalog growth must
measure serialized list size and introduce an explicit paginated discovery shape
before a list can exceed its bounded response, without silently truncating names.

### Stale-read recovery correction

Independent Code QA identified that generic tool failure hid the actionable stale
snapshot condition. `orders.read` now returns only the finite safe object
`{"error":"stale","restart":true}` for a changed version or snapshot hash. The
host persists the stale error alongside this result and records an error/conflict
diagnostic. JavaScript and the next model prompt can distinguish it from an
unknown transport outcome: discard all accumulated chunks, then restart at the
first chunk. No arbitrary backend error text is exposed. Other failures and all
mutation receipt semantics remain unchanged.

## Implemented pass and massage read slice (27 tools)

Current source exposes 27 operations, or 25 without booking permission. This adds
passes.events/get/invitations/event.read and massage.parties/slots/bookings/provider.read.
The read methods require a current authenticated principal; can_book does not gate
public navigation or own data. Mutations and restricted admin/staff reads remain
outside this slice. All-domain parity remains the end state, not an acceptance claim.

`bot/script_domain_reads.go:177` dispatches through typed existing clients and four
fixed navigation/detail endpoints. `passbooking/navigation.go` and
`massage/navigation.go:42` authorize before load and output. Core pagination avoids
loading full event titles or provider about text merely to navigate. The detail
operations use `core/read_navigation.go:76` and native JSON string chunks, with a
1 MiB serialized resource limit and finite result_limit when unavailable. Excerpts
are explicit and full detail remains available up to that stated cap. Existing
Core bulk endpoints and the 1 MiB APIClient cap are unchanged.

Navigation caps: 20 items / 24 KiB for new Core event/slot pages; 20 items / 32 KiB
for existing typed invitation/party/own-booking script pages. Details use 4000-rune
chunks. Actor, operation and query/resource are bound to continuations. Changed
slot/detail snapshots return safe stale/restart, with durable conflict evidence.
Pass events use live keyset pagination. Successful payloads remain in private
receipts but are omitted from subsequent model prompts. Empty complete pages
produce finite no_results diagnostics with counts, never content.

Regression evidence covers EN/RU, reads with can_book=false, cross-owner and
cross-event/query cursors, >1 MiB aggregate event/provider data, exact long-title
and localized-about reconstruction, stale restart and explicit detail size errors.
No provider work schedules, notification settings or foreign appointments enter
these read results. Eight business calls and final 4 KiB results remain unchanged;
large traversals require explicit incompleteness and bounded continuation handling.

### Product manifest for the isolated QA candidate

New files (all under platform/internal): core/read_navigation.go;
passbooking/navigation.go; massage/navigation.go; api/pass_navigation.go;
api/massage_navigation.go; bot/script_domain_reads.go.

Edited product files: massage/read.go (preserved Slots wrapper and optional
navigation projection); bot/script_registry.go (catalog/projection);
bot/script_tools.go (finite size error and page metadata); bot/script_reads.go
(two tool-name constants only); api/api.go (only the following registrations):

```go
massageNavigationRoutes(business, massage.Service{DB: s.DB}, logger)
passNavigationRoutes(business, passbooking.Service{DB: s.DB}, logger)
```

Insert after massageRoutes and passBookingRoutes respectively. Other current
api.go hooks belong to sibling work and are not part of this candidate. No new
migration, configuration, dependency, protocol or sibling runtime change is required.

New tests: internal/bot/script_domain_reads_internal_test.go and
integration/script_domain_reads_test.go. Edited tests:
internal/bot/script_registry_internal_test.go; integration/script_tools_test.go;
integration/script_reads_test.go; integration/script_profile_test.go.

### Next concrete slice

Add current domain capability evidence for privileged reads before binding them:
pass payment queue/history, massage practitioner own schedule/preferences and
assigned bookings. Reuse typed clients and domain ACLs; can_book is not an admin
or practitioner grant. Add bounded pages/chunks where existing collections could
exceed transport bounds. Then expose existing agent-authorized mutations through
shared preparation/receipt admission while preserving manual confirmation.
Service-only APIs stay internal. Measure each discovery expansion; add explicit
paginated discovery before its byte budget is reached, never truncate names.
Final measured catalog: 27 tools; full help descriptors 8472 bytes, name-only
bindings 1149 bytes, list metadata 4375 bytes. No discovery pagination is needed
at this measured size. Focused real-PG script/domain/massage tests passed (22.957s),
complete internal/massage tests passed, and scoped vet passed. Pinned scoped lint
loaded all affected packages and reported zero owned-file findings; its remaining
10 findings were in concurrent pass/legacy work, outside the candidate manifest.
Candidate9 subsequently passed isolated project lint and both fresh reviews:
`qa.local/domain-reads-codeqa/report.md` and
`qa.local/domain-reads-fqa/report.md`. This accepts the public/owner read slice,
not the privileged reads and mutations described above. Functional limitations
include deterministic provider plans, emulated touch, and no timed intra-run
ACL race or identical transport-update replay injection.
