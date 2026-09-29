# Agent and Sobek business API coverage

Research date: 2026-09-27. This is a source inventory of the live Go tree, not an acceptance report or a promise that a frozen deployment contains these bindings. No implementation or gate runs form part of this research. API availability, typed agent intent, and Sobek availability are separate columns: acceptance of one does not establish the others.

## Priority gaps

1. **Food Sobek tools are integrated.** Owner view/quote/change/payment preparation, scoped reviewer queue/read/decide/proof and CSV export passed independent Code QA and Functional QA on the frozen candidate. Current event admission, live rights, separate payment generations, complete reviewer reads, proof replacement and durable export continuation remain enforced. Combined-product verification is separate.
2. **Massage mutation gap is closed in the live Go tree.** `massage.book`, `massage.cancel`, `massage.practitioner.instant` and `massage.practitioner.configure` passed both independent gates and were integrated. Whole-product acceptance remains separate.
3. **Model settings bindings are integrated.** `models.effective`, scoped own/others/global get/set and `models.grants.set` passed independent Code QA, semantic/mouse Functional QA and a separate EN/RU touch gate. Current authority, host versions, stale/restart and replay remain enforced. Whole-composition acceptance is separate.
4. **Pass operations are integrated in Sobek.** Registration, administrator actions, payment review, takeover, durable batches, tier reads and XLSX export passed both independent gates. Host-bound observations, queue provenance, current authority and durable operation references preserve grounding and retries.
5. **Full modern-order tools are integrated in accepted1222.** Event discovery, complete catalog/history/order reads, full-choice drafts, quoting, owner mutations, payment review/proof display and XLSX delivery passed the recorded modern-order Code/Functional QA scope. Shared knowledge bindings are also integrated: scoped fact/proposal reads, curation, private memos and manual review cards. Whole-product acceptance remains separate.
6. **Broadcast review/results and native continuation are integrated.** `broadcasts.review` returns complete bounded owner-authorized campaign pages; `broadcasts.show` displays the current manual controls and may resume preparation. Both independent gates passed. Sending still requires manual confirmation; `broadcasts.cancel` remains pending-input cancellation.
7. **Credit accounting is integrated.** Owner usage/history and separately authorized administrator usage/history/default/policy tools use the authoritative ledger. Explicit cutover preserves each update's legacy/credit mode. Unknown costs are never represented as free; whole-product and real-provider acceptance remain separate.

## Existing discovery and authority contract

[The registry](../platform/internal/bot/script_registry.go) assembles workflow/order, memory, profile, domain read, privileged read and broadcast entries. `tools.$list()` lists currently authorized names/descriptions. `tools.namespace.method.$help()` returns that tool's live schema. Dispatch reconstructs the same authorized registry, so knowing a hidden name does not authorize a call. A running script is also restricted to its initial binding set; a new grant is available on a later run, while revocation removes access immediately. See [scope restriction](../platform/internal/bot/script_scope.go), [dispatch](../platform/internal/bot/script_tools.go) and [Sobek host callbacks](../platform/internal/scriptworker/execute.go).

This is the correct extension point. Add explicit business bindings, not arbitrary HTTP, SQL or service-token access. Each new privileged entry must be absent from discovery **and help** when unauthorized. Execution must check current rights again, including the specific event/target, and mutations must retain the domain transaction's authorization checks. Do not infer authority from archived history, a displayed role name, a cached capability, or `can_book` alone.

The host owns authenticated actor, replay keys and authoritative versions. Existing media paths also bind actual uploaded media and payment generation. A model supplies intent and authorized target selection, not those authority fields. Read results remain untrusted content. Tool names and generic errors should not disclose inaccessible operations or records.

## Coverage inventory

`S` means an existing Sobek binding; `P` means an existing typed Plan intent or host-provided planning context; `M` means a manual/browser route without a general agent binding; `H` means a host/service boundary that should remain outside direct agent calls. Names below describe current code, not proposed tools. Paths in a row share the indicated prefix only where explicitly stated.

### Workflow, modern orders, profiles and history

Sources: [API composition](../platform/internal/api/api.go), [orders API](../platform/internal/api/orders.go), [proof API](../platform/internal/api/proofs.go), [order export](../platform/internal/api/export.go), [script tools](../platform/internal/bot/script_tools.go), [paged reads](../platform/internal/bot/script_reads.go), [order Plan executor](../platform/internal/bot/order_agent.go).

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `GET /v1/business-capabilities` | P: host-populated capabilities; registry authorization | H: authority input, not a model-supplied grant |
| `GET /v1/workflow`, `GET /v1/catalog` | S: `workflow.get`, `workflow.catalog` | Covered reads |
| `POST /v1/actions` | S: `workflow.select`; P: generic action | Selection creates a draft; confirmation remains manual. Do not add an automatic confirm binding |
| `GET /v1/orders/{order}`, `GET /v1/order-events/{event}/orders`, `GET /v1/order-events/{event}/orders/{order}` | S: existing compact `orders.list/get/page/read`, plus `orders.browse` and complete `orders.inspect`; P: order summaries | Explicit event selection comes from `orders.events`; full reads bind observed owner, version and payment attempt |
| `GET /v1/order-events/{event}`, `GET /v1/order-events/{event}/history` | S: `orders.events`, `orders.event`, `orders.history.page`, `orders.history.read` | Complete bounded catalog/history chunks, cursor continuation and stale-snapshot checks; history includes retained deleted orders |
| `GET /v1/order-events/{event}/admins`, `.../payment-inbox` | S: `orders.contacts`, privileged `orders.inbox` and `orders.review.read` | Contacts do not confer review authority; privileged discovery/help remains hidden without current rights |
| `POST /v1/order-events/{event}/quote` | S: `orders.choice`, `orders.quote` | Host-owned immutable choice references support large drafts; quote is not a reservation |
| `POST /v1/order-actions` | S: compact `orders.change`; full `orders.update` for create/edit/delete/cash/cancel_proof/country; privileged `orders.review.decide` for accept/reject | Host binds identity/version/replay and current observation. Receipt upload remains host-owned |
| `GET /v1/order-events/{event}/orders/{order}/payment-instructions` | S: `orders.instructions`; P: `order_action` `payment_instructions` | Requires completed owner inspection; bounded full instructions |
| `GET /v1/order-events/{event}/export` | S: privileged `orders.export`; P: `order_action` `export`; M: XLSX delivery | Exact current-event artifact delivery; uncertain sends are not automatically retried |
| `POST /v1/order-proofs`, `GET .../orders/{order}/proof`, `GET .../orders/{order}/proof/file` | S: `orders.proof`, privileged `orders.review.proof`; P: receipt intent through host media | Display uses the completed read's bound receipt. Raw upload/promotion and arbitrary proof IDs/bytes remain outside script authority |
| `POST /v1/legacy-order-callbacks/resolve` | H/M: old callback compatibility | No new agent wrapper needed; use semantic operations |
| `GET /v1/me/preferences`, `PUT /v1/me/preferences/language` | S: `preferences.get`, `preferences.setLanguage`; M: language change | Integrated owner-scoped setter; preserve explicit user language choice |
| `GET /v1/me/pass-profile`, `GET /v1/me/pass-profile/history`, `POST /v1/me/pass-profile/actions` | S: `profile.get`, `profile.set`, `profile.history`; P: `profile_action` | Integrated after privacy and EN/RU interaction gates; host owns version/replay |
| `GET /v1/me/history`, `GET /v1/me/history/context` | S: `history.page` and complete `history.read`; P: `history_action` and host context | Owner-only bounded reads and deletion generations; conversation history is distinct from `orders.history.read` |

### Legacy food

Sources: [food API](../platform/internal/api/legacy_food.go), [commands](../platform/internal/legacyfood/mutations.go), [current public media contract](food-agent-contract.md). The accepted food bindings below are integrated in the live Go tree.

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `GET /v1/food/view?event=&order=` | S: `food.view` | Complete bounded owner view; new calls require current-event evidence |
| `POST /v1/food/quote` | S: `food.quote`; M: browser menu | Authoritative catalogue validation and totals |
| `POST /v1/food/commands`: `save_meals`, `delete_meals`, `toggle_activity` | S: `food.change` | Existing validation, locks, capacity and host-bound versions |
| Same endpoint: `begin_payment`, `prepare_activities` | S: `food.payment.prepare` | Native payment preparation; separate meal/activity states and durable latest hint |
| Same endpoint: `submit_proof` | P: `media_action` with explicit current `media_id`, `order_id`, `food_kind` | Narrow coverage. Host binds event/order/version/kind/generation and verifies current user evidence. Never auto-attach from an unrelated message or pending hint |
| `GET /v1/food/review?event=&order=`; commands `accept`, `reject` | S: `food.review.queue`, `food.review.read`, `food.review.decide` | Current scoped rights; complete sequential observation before explicit decision |
| `GET /v1/food/events/{event}/export` | S: `food.export`; M: two CSV deliveries | Owner-bound durable continuation, exact artifacts; separate from modern XLSX |
| `GET /v1/food/events/{event}/orders/{order}/proof` | S: `food.review.proof`; M: receipt display | Conditional observed version/generation and current rights; no proof bytes in script result |
| `POST /v1/food/legacy-menu`, `POST /v1/food/legacy-callbacks/resolve` | H/M: compatibility adapters | Bind semantic quote/commands, not legacy callback strings |

Food receipt selection uses `food_kind` values `meals` or `activities`; there is no model-writable `food_event` in the current Plan. Both kinds named, an uncertain choice, or an unavailable target must not become an inferred receipt submission. Use current host-displayed target evidence and refresh stale state before another explicit choice.

### Passes and lineup

Sources: [pass routes](../platform/internal/api/pass_booking.go), [Plan validation](../platform/internal/agent/pass_registration.go), [domain read tools](../platform/internal/bot/script_domain_reads.go), [privileged read tools](../platform/internal/bot/script_privileged_reads.go), [batch API](../platform/internal/api/pass_batch.go), [durable batch service](../platform/internal/passbooking/admin_batch_runtime.go), [export](../platform/internal/api/pass_export.go), [lineup reads](../platform/internal/bot/lineup_reads.go).

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `GET /v1/passes/events`, `/v1/passes/events/page`, `/v1/passes/events/{event}/detail` | S: `passes.events`, `passes.event.read`; P: registration event reads | Covered via bounded pages/full detail chunks |
| `GET /v1/passes/events/{event}/me`, `.../invitations` | S: `passes.get`, `passes.invitations`; P: registration reads | Covered |
| `GET .../{event}/payment-admins`, `.../queue`, `.../capabilities` | S: `passes.registration.read`, `passes.admin.queue` | Host-grounded ordinary and current administrator views |
| `POST /v1/passes/actions` | P: `solo`, `invite`, `accept`, `decline`, `payment_admin`, `cancel`, `admin_cancel`, `admin_uncouple`, `recalculate`, `proof_accept`, `proof_reject`, `takeover`, `received_only` | S: registration/admin/payment/takeover semantic bindings reuse existing host/domain operations |
| `GET .../{event}/admin/targets/{telegram_id}`, `POST /v1/passes/admin/assign`, `GET .../{event}/takeover/{telegram_id}` | P: admin target/takeover reads and `admin_assign` | S: `passes.admin.target`, `passes.admin.assign`, `passes.takeover.read`; host-grounded targets and versions |
| `GET .../{event}/me/payment-quote`, `GET .../{event}/participants/{owner}/payment`, `GET .../{event}/participants/{owner}/payment/file`, `POST /v1/pass-proofs` | P: payment view and media receipt intent; M: file display | Quote/payment metadata could be bounded reads; raw proof intake stays host-owned |
| `GET .../{event}/payment-queue`, `GET .../{event}/payment-history` | S: `passes.payments.queue`, `passes.payments.history`; P: payment queue view | Current event payment-read rights; history metadata does not grant access to proof bytes |
| `GET /v1/privileged-read-capabilities`, `GET /v1/privileged-read-events` | Host capability check; S: `privileges.events` | Event discovery exists, including relevant historical events |
| `GET /v1/passes/events/{event}/tiers`, `POST /v1/passes/batches` | S: `passes.tiers`, `passes.batch.assign/cancel/uncouple` | Existing durable batch service; per-recipient results and immutable replay input |
| `GET /v1/passes/export` | S: `passes.export`; M: `/passes_table` XLSX | Authorized exact artifact delivery; durable operations/resume and uncertainty |
| Lineup current/day/full, date/room/DJ/cursor query | S: `lineup.query`; P: `lineup_action`; shared runtime provider | Integrated after independent Code and Functional QA. Shared four-query budget, bounded pages and host-bound cursors; whole-composition acceptance remains separate |

### Massage

Sources: [API](../platform/internal/api/massage.go), [navigation](../platform/internal/api/massage_navigation.go), [legacy adapters](../platform/internal/api/massage_legacy.go), [commands](../platform/internal/massage/commands.go), [privileged reads](../platform/internal/api/privileged_reads.go).

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `GET /v1/massage/parties`, `/slots`, `/slots/page`, `/bookings`, `/providers/{provider}/detail` | S: `massage.parties`, `massage.slots`, `massage.bookings`, `massage.provider.read` | Covered bounded reads |
| `GET /v1/massage/provider-names`, `/timetable` | M/browser data; provider information partly reachable through other reads | No exact timetable/provider-list tool; add only if existing party/slot/detail reads cannot answer required queries |
| `POST /v1/massage/actions`: `book`, `instant`, `cancel` | S: `massage.book`, `massage.practitioner.instant`, `massage.cancel` | Host binds absolute start, identity, version and replay key; domain checks availability and current rights |
| `GET /v1/massage/preferences`, `PUT /v1/massage/preferences` | S: `massage.practitioner.preferences`, `massage.practitioner.configure` | Both preference booleans required; current event membership, private owner and hidden unauthorized tools/help |
| `GET /v1/massage/practitioner/schedule`, `/v1/massage/practitioner/bookings` | S: `massage.practitioner.schedule`, `massage.practitioner.bookings` | Live practitioner authority required |
| `POST /v1/massage/legacy-instant`, `GET /v1/massage/legacy-practitioner-booking`, `POST /v1/massage/legacy-preferences`, `GET/POST /v1/massage/legacy-draft` | M/H: legacy UI adapters | Prefer semantic booking/preference operations; do not expose old callback/draft protocol as the main agent API |

### Broadcasts, model settings and administration

Sources: [broadcast tools](../platform/internal/bot/script_admin_broadcast.go), [audience tools](../platform/internal/bot/admin_message_audience.go), [broadcast API](../platform/internal/api/admin_broadcast.go), [campaign actions](../platform/internal/api/admin_message.go), [model routes](../platform/internal/api/model_settings.go), [model authority](../platform/internal/modelsettings/service.go), [admin utilities](../platform/internal/api/admin_utilities.go).

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `POST /v1/admin-messages/audience`, `/profile` | S: `broadcasts.audience`, `broadcasts.profile` | Bounded recipient enumeration and full authorized profile chunks support local JS filtering, then explicit recipient IDs |
| `POST /v1/admin-messages/preview` | S: `broadcasts.preview` | Preview only; does not authorize sending |
| `POST /v1/admin-messages/input/start`, `/input/prompt`, `/input/pending`, `/input/attach`, `/input/cancel` | S: preview/input workflow, `broadcasts.pending`, `broadcasts.attach`, `broadcasts.cancel`; host prompt transport | Source owner/chat/message bound by host. Pending input cancellation differs from campaign cancellation |
| `POST /v1/admin-messages/capabilities` | Host registry check | All broadcast tools hidden without current permission |
| `POST /v1/admin-messages/review`; `POST /v1/admin-messages/{id}/{action}` with `results`, `cancel`, `resume`, `send` | M: review/campaign controls | Missing review/results and campaign-control intents. **Sending must remain an explicit manual confirmation.** A tool may render its review/continuation card, not invoke `send` |
| `GET /v1/model-settings/effective`, `/permissions` | S/host | `models.effective`; current permissions drive hidden names/help |
| `GET/POST /v1/model-settings`, `/v1/model-settings/default`, `/v1/model-settings/users/{owner}` | S | `models.own`, `models.others`, `models.global` get/set; current scope rights, host versions and durable keys |
| `POST /v1/model-settings/grants` | S | `models.grants.set`; canonical superadmin only, bounded model capabilities |
| `GET /v1/admin-utilities/authorize`, `/v1/admin-utilities/users/{id}` | M | Add a scoped lookup only if broadcast profile/pass target reads do not cover the requested purpose; never create a universal private-user read |
| `POST /v1/admin-utilities/refresh` | M: administrative refresh | Operational mutation, not a user data tool. A proposed admin binding needs an explicit narrow description and the same live superadmin check |

### Knowledge, memory and quotas

Sources: [knowledge API](../platform/internal/api/knowledge.go), [typed knowledge intent](../platform/internal/agent/knowledge.go), [knowledge binding](../platform/internal/bot/knowledge_actions.go), [memory API](../platform/internal/api/memory.go), [memory tools](../platform/internal/bot/memory_tools.go), [quota](../platform/internal/bot/agent_quota.go), [credits research/status](credits-accounting.md).

| Public operation | Current agent coverage | Gap or boundary |
| --- | --- | --- |
| `GET /v1/knowledge`, `/page`, `/fact`, `/scopes`, `/proposals` | S: `knowledge.scopes`, `knowledge.read`, `knowledge.proposals`, authorized `knowledge.review_queue` | Live scoped authority and bounded continuation |
| `GET /v1/me/memos`, `/v1/me/memos/{key}` | S: `knowledge.memos`, `knowledge.memo_read` | Owner-private, deletion-aware reads |
| `POST /v1/knowledge/actions` | S: `knowledge.suggest`, `knowledge.curate`, `knowledge.remove_fact`, `knowledge.memo_set`, `knowledge.memo_delete`, `knowledge.review_card`; private documents use `memory.write` | Not interchangeable scopes. Proposal review is presented as a review card; preserve manual review decisions rather than adding an ungrounded approval tool |
| `GET /v1/memory/summary`, `/index`, `/search`, `/read`, `/history`, `/revision`, `/sources` | S: `memory.summary`, `memory.index`, `memory.search`, `memory.read`, `memory.history`, `memory.revision`, `memory.sources` | Existing literal/regex/lexical search and cursor interfaces should be reused |
| `GET /v1/memory/document`, `/deletions` | Host state/version/deletion context; private document mutation uses `memory.write` | Not missing end-user operations merely because these synchronization reads lack one-to-one tools |
| Current question quota | P: `assistant_questions_remaining`; host rolling-24h reservation | No quota HTTP API/tool. Count reservation is not money; replay/failure semantics must remain unchanged |
| Credits, balance, usage, policy | S: `credits.usage/history`, `credits.admin.usage/history/default/policy`; authenticated API/manual controls | Current accounting authority; exact amounts, unknown/reserved distinction, no arbitrary ledger adjustment tools |

Credit tools use monetary OpenAI API-balance units, with current superadmins unlimited but accounted. Owner usage distinguishes settled, reserved and unknown amounts within its UTC month. Administrator report/policy names are hidden from ordinary users. Operator reconciliation is a separate authenticated administrative workflow, not a generic model ledger mutation.

## Intentional non-tool boundaries

`/internal/*` identity provisioning/authorizer, browser recipient authentication, Telegram metadata, notification claims/completions, broadcast source/delivery, pass announcements, memory provenance/assessment/archive and history summarization are authenticated host/service workflows. They are not omissions to fix with agent tools. Neither `/healthz` nor model/media/script worker transport is an end-user business action.

`POST /v1/media`, `GET /v1/media/{id}`, `GET /v1/media/{id}/file`, `POST /v1/media/{id}/proof` support host intake and authorized media use. Typed `media_action` covers receipt/clarify/other/cancel/avatar/video-inspection intent. A semantic request may use media the host actually supplied; it must not manufacture a Telegram file ID, retrieve another owner's bytes, or call proof promotion directly.

## Extension rules and ergonomics

Named bindings above already exist. Remaining implementation and acceptance work is tracked in PROGRESS, not this historical rollout sequence. Extensions should reuse registry descriptors, typed clients and durable host preparers/executors. Add event-specific capabilities only when an authoritative operation needs them; do not duplicate domain validation. Keep broadcast send and workflow confirmation manual. Use the credit ledger for monetary usage and preserve the explicitly selected legacy quota mode during cutover.

Today a script has eight host calls and a seven-second aggregate tool budget. Ordinary results have a 2KiB limit; selected paged/chunked reads have larger bounded limits. Discovery/help consumes real interactions; a large recipient audit cannot safely assume it can fetch every full profile in one script. Preserve cursors and explicit continuation instead of silently treating the first page as the whole dataset.

Useful small improvements are an existing-service-backed bounded multi-profile read for broadcast selection, bounded multi-target pass reads, and the already durable pass batch mutation. Each item must retain its own authorization/outcome; unknown or inaccessible recipients must not be silently dropped. Keep long resource fields in existing complete chunk reads and use compact page summaries for selection. Reuse current search/filter arguments before adding a new query language or generic batch framework.

Acceptance for each added binding should prove discovery/help hiding, execution after revocation, exact replay, stale target handling, bounded pagination and truthful partial results. Exercise both EN/RU user intents where interpretation matters, plus unrelated/ambiguous media messages. Required manual confirmations must still be visible and necessary. These are proposed acceptance conditions, not checks performed by this research.




