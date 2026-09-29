# Stage B: final composition and complete order-extras interaction

Updated 2026-09-27. Stage A and service composition B1 are accepted; B1 is integrated in main1222. The final Stage B candidate combines verified privacy corrections, the independent application boundary (B2), final saved-state ownership (B3) and the complete orders coordinator (B4). Build and check intermediate steps, then obtain fresh Code QA and source-blind Functional QA for the combined candidate. Temporary adapters do not each require a separate final acceptance cycle.

## Premise and baseline

No Go branch has reached production. Internal APIs, schema and saved-state formats may change; the Python forward importer must target the final model. Preserve required behavior, source data, authority and final-format durable replay. Do not build aliases, forwarding layers or old-format readers solely for prerelease Go snapshots.

Use accepted main as the behavioral baseline and inventory pending privacy work before implementation. Its requirements and verified defects belong in the combined candidate; a separately accepted privacy/B2 snapshot is not a prerequisite for B3/B4. Frozen QA snapshots remain immutable. Work in a separate candidate with explicit file ownership, and retain failed/pending evidence as such.

The historical dependency map below describes pre-B1 snapshot **qa.local/composed-choice-history-privacy/source**, 1218 files, manifest digest **b389873237a838d129418877f11986afa46daf630a343c494d1e709aa1f5575b**. B1 already removed hidden HTTP service construction. Refresh these locations against the implementation baseline; they identify responsibilities, not required final paths or signatures. The B2 draft and preflights are reusable implementation evidence, not final Stage B acceptance.

## Complete scenario

Existing unpaid/cash order extra edit: manual order selection → agent adds/removes one current extra → Mini App edits the same order → refreshed Telegram cards. Include two editable orders, explicit selection, stale callback and restart replay. All command paths converge on the existing authoritative orders operation.

The application owns context binding, durable winner/retry/terminal policy, execution and expected failure recording. Telegram owns parsing, callbacks, localization, page/card state and send/edit. Orders owns live authorization, SQL and atomic effects. The full model/script host belongs to C; lifecycle belongs to D. Moving only a client or binder does not complete B.

## Historical dependency inventory

| Boundary | Frozen source evidence | Historical dependency / responsibility |
| --- | --- | --- |
| Combined runtime | `cmd/zns/app.go:27 runApp`, `:63` bot/client construction, `:98 AuthenticatedHandler`, `:119 appMux` | Constructs Bot, helpers and local HTTP client but gives HTTP a `core.Service` from which other services are constructed. Historical loopback transport; preserve its identity checks in the replacement. |
| Split runtime and gateway | `cmd/zns/api.go:22 runAPI`; `cmd/zns/main.go:114` client literal and `:167 botGateway` | Separate entry points repeat composition. `botGateway` takes a Bot to obtain API/onboarding/active-event dependencies. |
| HTTP assembly | `internal/api/api.go:64 AuthenticatedHandler` | Builds conversation, knowledge, media, massage, profiles, registration, orders, legacy food and admin-message services. Health and known-owner checks use `s.DB`; `/v1/` verifies bearer and installs the owner context. Service-authenticated routes remain separate. |
| Hidden HTTP assembly | `internal/api/admin_utilities.go:12 adminUtilityRoutes`, `model_settings.go:11 modelSettingsRoutes`, `credits.go:13 creditsRoutes`, `privileged_reads.go:12 privilegedReadRoutes` | Each constructs additional service values from `core.Service.DB`; replacing only the top-level literals would leave the composition boundary incomplete. |
| Order HTTP adapter | `internal/api/orders.go:10 orderRoutes` | Already receives concrete `orders.Service`; adapts quote/get/list/action requests. Keep `DecodeOrderRequest`, pagination, stable error codes and large-payload transport routes. |
| Client identity/transport | `internal/bot/client.go:17 APIClient`, `:36 httpClient`, `:57 request`, `:65 requestToken`; `auth.go:58 AuthenticateTelegram`, `:84 notificationContext`, `:98 userToken` | Concrete client owns verified principal context, per-request exchange, endpoint-scoped/no-redirect HTTP and response limits. `authenticatedUpdate` at `auth.go:21` additionally rejects inactive users before private input/model work. |
| Typed order client | `internal/bot/orders_client.go:12 OrderEvent`, `:21 Orders`, `:25 Order`, `:38 QuoteOrder`, `:48 ExecuteOrder`, `:96 OrderHistory`; `order_catalog_client.go`, `order_history_client.go` | OrderEvent/OrderHistory have bounded large-response fallback and integrity/fingerprint checks. Preserve bounded results and integrity; retain transport fallback only for HTTP consumers that still need it. |
| Mini App consumer | `internal/miniapp/handler.go:28 Gateway`, `:105 AuthenticateTelegram`, `:136 read`, `:161 quote`, `:178 save`, `:218 forOrder` | Depends on `bot.APIClient` and `bot.TelegramOnboarding`. Save creates an origin=manual version-bound `orders.Command`; event resolution must remain owner-bound. |
| Other enabled Mini App routes | `internal/miniapp/timetable.go:35 timetable`; `legacy_food.go:17 foodRoutes`; `legacy_food_post.go:17 legacyFoodPost`; `identity_provisioning.go:13 onboardVerified` | Uses timetable, FoodView/FoodCommand/FoodQuote/FoodLegacyMenu and identity onboarding. An orders-only Gateway interface would break live routes. |
| Telegram order admission | `internal/bot/orders.go:33 handleOrders`, `:59 handleOrderCallback`, `:111 orderButton` | Owner-scoped token resolves a stored command from `bot.order_buttons`; dispatch derives `tg-order-<update>`. Special export/instructions/upload/show-proof branches are not mutations and remain Telegram adapters. |
| Agent order context and binder | `internal/bot/order_agent.go:89 addOrderContext`, `:138 orderSummaries`, `:156 proposedOrderCommand`, `:207 proposedInstructions`; `plan_binding.go:9 bindPlanCommands` | Bounded current-event summaries and full current order re-read. More than one editable order requires original request evidence naming the target. Script-refreshed versions are used, but original utterance/voice evidence is retained. |
| Durable turn | `internal/bot/bot.go:98 cachedPlan`, `:368 planForUpdate`, `:408 createAllowedPlan`, `:320 executePlan` | `bot.replies` saves full typed plan JSON; conflict returns the already stored winner. Restore precedes model invocation. Orders dispatch derives the same per-update key as manual callback. Envelope includes registration/media/profile/history fields and cannot be reconstructed from an orders-only projection. |
| Completion and UI | `internal/bot/order_agent.go:19 handleAgentUpdate`; `orders.go:88 executeOrder`, `:132 RenderOrders`, `:178 renderOrderMenu`; `order_cards.go`, `order_paging.go`, `orders_locale.go` | User archive/finalized memory/input record precede execution; expected <500 domain errors get `order_error`; success focuses page; reply persistence/archive and rendering follow. Telegram transport failures are separate from domain commit. |
| Privacy dependencies of durable replay | `internal/bot/history_deletions.go:135 validateHistoryPlan`, `:166 validateHistoryInteractions`; `history_reply.go:13 historyReplyVisible`; `history_capture.go:100 archiveAssistantReply`; `bot.go:61 record` | Current generation gates restore/save/execute/read. Stale saved plans become durable terminal markers and stale renderable outputs are deleted in the same transaction. Assistant archive has an atomic expected-generation fence. Do not replace this with one initial GET. |
| Current script order lane | `internal/bot/script_order_choice_store.go currentModernChoice/claimModernChoice/admitModernChoice`; `script_tools_store.go bindScriptToolKey`; `script_modern_orders.go` | Host-bound uncommitted choice generations/cursors/catalog/version and durable claims; commit carries generation into the domain lock. Keep this lane in its current host for B; it must use the shared authenticated application boundary and authoritative operation. |
| Domain transaction | `internal/orders/service.go:222 Execute`, `:283 Command.validate`, `:306 operation.authorize`; `internal/conversation/write.go:69 LockGeneration` | Actor/event locks and current authorization precede receipt replay. New derived mutations take the history deletion lock after replay and retain it through commit. No structural move may reorder these. |

## Final boundaries

### Runtime and authenticated application API

Reuse accepted B1 ready-service composition. Runtime constructs concrete dependencies; HTTP and synthetic stands receive them. No hidden service construction, DI container or generic command bus.

Prefer typed in-process calls for bot/assistant and Gateway server code. Keep HTTP for the browser and genuine external consumers. Inventory external consumers before deleting routes, split-mode entry points or large-response transport helpers. Mini App must no longer import bot; give it a small consumer-owned contract for its actual order, timetable, food and onboarding operations. Retain their enabled behavior and owner-bound event resolution, not the historical client signatures.

Extract one explicit authorizer before replacing loopback. Initially retain trusted Telegram/initData/browser admission, principal-owner agreement, per-operation delegated token exchange, configured verification/introspection including issuer/audience/actor, known local owner and live domain/object checks. Deny inactive provider users before private intake/model work. Keep provider outages retryable. A typed principal comes from this authorizer, never model arguments. User operations must not receive delivery, provenance, metadata or provisioning capabilities.

Both HTTP and in-process entry points use this boundary. Prove equivalent identity and permission behavior before removing the old path. Removing exchange/introspection is a separate identity-policy decision. Remaining network adapters retain credential isolation, no redirects, decoding/input limits and explicit sandbox-only signer mode. Typed calls retain business/model result bounds even when HTTP chunk/reassembly code disappears.

Do not retain a permanent bot.APIClient forwarding shell or duplicate principal implementation. Temporary build adapters may exist within the candidate; remove them when their callers move. The B2 draft is useful source material, not a commitment to a permanent loopback client.

### Orders coordinator

Use a small concrete application owner, for example internal/interaction/orders:

- Context and binding receive a verified actor, bounded observed orders/catalog and original text/spoken selection evidence. They enforce explicit target selection and bind current observed versions; model proposals supply no actor/key/price authority.
- Resume-or-plan delegates to one shared turn store and invokes the model factory only without a saved winner or terminal outcome. Execute-saved uses the bound command and stable key. Manual and agent mutations share this execution policy; Mini App uses the same authoritative application/domain operation.
- Return typed outcomes for committed orders and expected domain refusals. Record required application/error state before handing presentation to Telegram. Infrastructure errors remain retryable. Keep read/export/proof intents distinct from mutations.

Update all callers together. An adapter remains only where it translates Telegram input/output, not to preserve an old Go method. Do not carry Telegram update structs into application contracts or hold a domain transaction while calling Telegram.

### One final durable turn and provenance model

One concrete store owns load, save-winner, terminal transitions and every plan-dependent reader, including system notices, reply visibility and consumed-voice resumption. Use an explicit final format/version with validated typed fields; unsupported versions and missing required authority fail closed. There is no requirement to round-trip prerelease JSON or preserve its tags.

Inventory the full semantic envelope: commands and outcomes, registration menu/access evidence with booking/invitation identities, history provenance, fixed system notices, profile/media evidence and AV references. An orders-only projection is insufficient. Represent absence/empty authority explicitly where they have different meanings. Keep authority identity comparison with its owner. Update script receipts and all affected readers together; do not write reduced receipts or maintain parallel old/new stores.

Keep save-winner concurrency, validation before restore/save/execute, atomic terminal redaction of renderable outputs, distinct history/access terminal semantics and retryable authority failures. Preserve the consumed-voice atomic predicate and cleanup success points. A cleanup failure must not cause a new model plan.

Use explicit original-input, derived-output and trusted system/domain-outcome history APIs. Derived writes require a host-bound expected generation, including zero, and fence against deletion in the append transaction. Do not infer provenance from missing plans, reply kind or an optional legacy flag. Imported assistant history can be original source evidence. Models/public callers cannot select a privileged provenance class.

Update forward import and fixtures to the final schema. Do not automatically delete development state; account for valuable fixtures and preserve source/QA evidence. Current-format cache invalidation after deletion remains required even when old Go upgrade branches disappear.

## Behavior and security invariants

1. Verified identity and live domain authority precede effects and receipt replay. Preserve user/service isolation and the inactive-user versus provider-outage distinction.
2. Preserve actor/event lock order, full-command receipt hash, replay after current authorization, and deletion lock for new derived mutations through commit. Keep post-lock clock/deadline, version, capacity, audit/outbox and reconciliation rules. A format/API redesign does not authorize weakening these guards.
3. Keep final-runtime actor/key idempotency, conflict on changed payload and unknown-commit replay. Do not rebind version, regenerate a key or call the model for an existing winning turn. Python business identity and permanent source references survive conversion.
4. Stale generation/access evidence cannot reach model context, renderable output or a new derived mutation. Terminal state is durable; committed business effects remain subject to current ACL. Bounded cleanup is not an authorization fence.
5. Preserve whole-batch inbox/cursor durability, global processing order and distinct plan/domain/reply/Telegram crash boundaries. No exactly-once external delivery claim, speculative parallel scheduler or outbox unification.

## Buildable steps and final gates

### Closure of the shared source contract

The shared provenance contract applies to every enabled consumer of a saved
model decision, including consumers outside the order domain. Closing these
paths is part of B privacy correctness; it does not complete the broader C
extraction of model orchestration or the D lifecycle refactor.

Before freezing B, account for these separate boundaries:

- Ordinary model plans and Sobek calls both retain their original generation
  and read evidence through domain execution. Validation at plan admission alone
  does not protect a later database commit.
- Saved single operations, partial batches and export continuations preserve
  their first admitted evidence. A new turn cannot replace that evidence with
  current grants or a new history generation. Results exposed by a resumed
  operation retain their original source identity as well.
- Language, model settings/grants, credit policies, orders/workflow, registration,
  profile, food and massage mutations use current target authorization and
  committed-receipt replay before checking sources for a new effect.
- Memory, proposals, summaries and broadcasts retain causal evidence when stored,
  copied, reviewed, rendered, published or queued. Explicit manual publication
  controls recipient access without erasing original provenance.
- Menus, proofs and file exports check current identity, the exact target scope
  represented by their content, and retained source evidence before external
  delivery, including edit-to-send fallback and resumed delivery. A remaining
  grant for one event must not authorize bytes from another revoked event.

Keep database effects and receipts atomic. External sends require fresh checks
at their boundary, but no database transaction may be extended across network
delivery to claim atomic revocation or exactly-once Telegram delivery. Preserve
and test the documented retry/ambiguity behavior.

### Implementation sequence

1. Record behavioral baseline, external-consumer inventory and pending privacy requirements. Agree final actor, turn/provenance and order contracts with their file owners.
2. Build shared authenticated application boundary from B1 dependencies; adapt Gateway and order callers. Verify security equivalence before removing loopback and unused forwarding/routes.
3. Implement final turn/store/provenance model and compose privacy corrections. Update every reader/writer, fixtures and forward importer; run focused winner/restart/deletion/access tests.
4. Complete order context/binding/resume/execution and manual-agent-Mini App handoff. Keep unrelated model/script orchestration out of this wave except necessary shared state callers.
5. Run required formatting/lint/build and focused real-PG/concurrency checks. Freeze the combined privacy+B2+B3+B4 candidate, then obtain fresh independent Code QA and source-blind Functional QA. Fix substantive findings and repeat affected fresh reviews.

Steps are buildable implementation checkpoints, not separately accepted releases. Use one owner for shared auth/runtime wiring and one for shared turn/receipt state; parallel order work starts after those contracts settle. Rollback uses a preceding source snapshot and matching disposable stand; do not require prerelease database upgrade compatibility. Only root updates PROGRESS.

## Focused behavior proof

This historical test inventory identifies behavior to preserve. Refresh names/paths and adapt tests to the final boundary; old passing results are not final acceptance. Drop assertions solely about obsolete Go formats/transports, retain their underlying privacy, authority, bounds and replay requirements.

| Concern | Existing proof to retain |
| --- | --- |
| Authoritative shared command/version | `integration/orders_api_test.go`: `TestOrdersHTTPContract`, `TestOrdersHTTPRejectsMalformedAndUnauthenticatedRequests`; `orders_bot_test.go`: `TestAgentContinuesManualOrderAndBindsVersionAcrossRetries`, `TestTelegramOrderCashLifecycleAndStaleCards`, `TestTelegramOrderButtonsAreOwnerScoped` |
| Mini App parity | `integration/miniapp_test.go`: `TestMiniAppUsesOwnerVersionPricesAndSharedAgentState`, `TestMiniAppRejectsUnsignedAndInjectedActions`, `TestMiniAppDeadlineDisablesEditorAndRejectsSave`; `modern_orders_miniapp_test.go`: `TestModernMiniAppFullChoiceGateway`; existing timetable/food gateway tests selected by actual changed methods |
| Locale/render history | `integration/orders_locale_test.go`: `TestOrdersLocaleRefreshAndRecipientNotifications`; existing old-card refresh tests; independent EN/RU UI acceptance remains necessary |
| Transaction/capacity | `integration/orders_test.go`: `TestOrderPaymentCapacityAndIsolation`, `TestOrderCashEditInvalidatesAttemptAndDoesNotReserve`, `TestOrderConcurrentProofCannotOversell`, `TestOrderReconciliationPreservesHistoricalPrices`; affected `orders_capacity_reservations_test.go` cases |
| Durable intake/retry | `integration/inbox_test.go`: `TestInboxPersistsBatchBeforeHandlingAndRecoversWithoutTelegram`, `TestInboxReplaysCommittedActionWithoutDuplicateEffects`; `inbox_process_test.go`: `TestInboxCrashRecovery`; these general tests do not replace the order-specific crash probes below |
| Real runtime and large transport | `TestModernChoiceFullRuntimeAcrossRestart`, `TestModernOrdersBoundedRuntime`, `TestModernChoiceMealRuntime`; large catalog/history and modern-choice boundary tests; client transport tests in `internal/bot/order_history_transport_internal_test.go` move with their owner |
| History privacy | `TestModernChoiceDeletedGenerationRejectsEveryUse`, `TestModernChoiceGenerationLegacyAndParentFailClosed`, `TestModernChoiceDeletionAtCommitBoundary`, `TestModernChoiceCurrentGenerationAndCommittedOrderSurvive`; `TestHistoryDerivedOrderMutationSharesDeletionLock`; existing reply visibility, archive-rejection, terminal-inbox and interrupted-plan tests |
| Identity | `internal/bot/auth_internal_test.go` and `download_auth_internal_test.go` retained under their final owner; `TestInactiveProviderUserCompletesDeniedUpdateWithoutReplay`, `TestProviderUserDeactivatedAfterAdmissionIsTerminal`, `TestProviderInfrastructureFailureKeepsDurableUpdate`; browser consent/restart/revocation tests |

Add only missing boundary proof:

- Final-format round trip and validation retain all required authority/provenance; concurrent saves return one winner; terminal redaction does not remove committed effects. No old-Go JSON compatibility fixture is required.
- Crash after saved bound plan, after domain commit before outcome persistence, and after outcome before Telegram completion. Restart without model invocation; prove one business mutation, unchanged bound command/key and current cards. Repeat with revoked rights and changed version.
- Manual selects an order, agent changes an extra, Mini App edits it, then old Telegram callback is rejected and cards refresh. Include two editable orders and original voice evidence.
- Compare HTTP and typed entry points for foreign/missing principal, inactive user before private intake, wrong issuer/audience/actor, revocation after discovery/binding, and attempted user access to host-only operations. Exercise retained network safeguards. Do not replace the actual identity verifier with a stub in the end-to-end security case.
- Cover final privacy/access evidence from pending work, including interrupted model/script calls, registration replay/discovery and deletion races. Acceptance applies to the resulting behavior, not to a particular intermediate patch.

Final Functional QA uses Telegram-like messages, callbacks, edits and Mini App in EN/RU. Cover stale cards, paid/proof edit rejection, multi-order selection, restart, identity/domain revocation and subsequent manual continuation. Use actual test Zitadel for required identity acceptance. Builder tests do not replace either independent gate.

## Collision and scope controls

The archived-pass inventory at qa.local/archived-pass-access-provenance/cumulative-delta-sha256.json is historical scope evidence, not acceptance. It overlaps pass menu/context/binding, plan authority, script receipts/registry, API navigation and registration SQL. Refresh its inventory before assigning writers.

One owner must reconcile shared turn, history and pass authority semantics. One owner must change script receipt/key binding; a fresh format cannot omit admitted-run, current-generation or access evidence. Shared auth/API/runtime files also have a single writer. Domain writers use agreed contracts rather than copy transport/principal code.

Do not change domain lock order, permission policy or Python import semantics merely to resolve a structural conflict. Pending verified defects may be fixed in the combined candidate; they need not be separately accepted first. Preserve immutable QA snapshots, and declare Stage B accepted only after the complete scenario, required checks and both fresh final gates pass.

Written by runtime_stage_d_preflight (GPT-6/Codex)
on behalf of Daniel Drizhuk
