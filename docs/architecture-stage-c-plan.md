# Stage C: interaction and agent-host ownership

Updated 2026-09-28. An isolated C1 development draft now runs in parallel with the remaining independent B checks. It does not accept B or change the main composition or frozen QA stands. Integrate or accept C only after the combined B candidate is accepted, or remaining blockers have concrete architectural deferrals recorded in [DEFERRED.md](../DEFERRED.md). Reconcile the draft against the final B source before freezing C. A deferral must identify the cause, the C change that removes it, remaining limitations and closure checks; failed checks never become passes. Root owns PROGRESS and the decision to freeze a successor.

Inspected baseline: `qa.local/architecture-stage-b-repair6/source`, manifest `final-manifest3.json`, SHA256 `247f01d4152d7089b4e438e7a42149ffb33daed8e6c9ce58dcdd7ad677c8237f` (manifest hash checked). Source references below are relative to that tree. This was source/document inspection only: no builds, DB operations, stand changes or ongoing FQA findings were used.

Requirements: [refactoring scope](architecture-refactor-plan.md), [ownership contract](architecture-ownership.md), [Stage B boundary](architecture-stage-b-plan.md). Earlier Stage C preflight compatibility aliases and old saved-format adapters are superseded. Preserve Python behavior/data and same-version recovery; do not preserve intermediate Go APIs for their own sake.

Candidate10 preparation refresh checked all 1474 files against manifest
`90516aea35ada8791971b73bcfb70cd5ae74e8fab84acb405e4de490ac90478a`.
The 13 paths changed since candidate9 preserve the domain owners below while
adding opaque provenance and history-window guarantees. This is a candidate
binding, not an accepted baseline. The exact caller and file-ownership inventory
is retained in `qa.local/architecture-stage-c-preflight/candidate10-delta.md`.
Bind implementation to the final accepted or explicitly qualified B composition.

Candidate11 adds only outgoing Telegram keyboard serialization and its tests
(`internal/telegram/send_json.go`, `send_json_test.go`). Its current binding is
`qa.local/architecture-stage-b-repair11/final-manifest2.json`, SHA256
`232b22b5962d21daa8e7ad610b084d9018c309943032601eec124f3548fcb804`, 1476 files.
This delta does not change the C1–C5 ownership map or application contracts.
Focused tests/vet/lint and independent Code QA passed; Functional QA remains
pending. It is a prospective baseline, not acceptance or permission to skip
the transition conditions above.

Canonical migration075 already owns author consent. Do not create another
submission schema. `Host.SubmitKnowledgeProposal` remains a separate manual
operation, inaccessible through model-selected knowledge commands. Existing
`LocalDerived` does not make every Host method local: derived knowledge still
uses HTTP in this candidate. Convert the actual callers rather than adding a
second local executor or exposing complete provenance through a new envelope.

## Target and existing foundation

### Mid-C architecture decision, 28 September

The [independent mid-C audit](architecture-mid-c-audit.md) confirms the modular
monolith and existing transaction owners. Include these completion conditions in
C; a package move alone does not satisfy them:

- Finish the full registration coordinator, including grounding, reads, manual,
  script and saved-command recovery. Keep the existing operations/status work.
- Move generic host authority, redaction, source aggregation, key binding and
  model-evidence policy out of Bot. Domain authorization remains domain-owned;
  Telegram retains input adaptation and presentation. Remove redundant forwarding
  ports where concrete coordinators suffice.
- Give the script ledger one owner for decoding admitted-operation records.
  Expose a narrow typed projection; derivedmutation must not depend on nested
  host JSON. The scenario layer combines projections with canonical receipts.
  Measure recent-list and exact-ID reads against a long synthetic history before
  acceptance. Do not add a normalized store without evidence that it is needed.
- Keep dependency direction interaction -> domain/application contracts, never
  interaction -> agenthost. Coordinators define their read/reservation needs;
  composition injects implementations. Reconcile account/workflow types directly
  without aliases and retain one source finalizer after original-request archive.
- Represent assessment deferral explicitly. A provider failure leaves the same
  proposal pending and retryable; it is not successful assessment. Preserve winner
  persistence, cancellation, safe diagnostics and separate manual author consent.
  Coordinator owns durable sequencing; the host adapter owns provider invocation,
  budget and source checks at actual requests.
- Integrate domain bounds and prove local/HTTP parity, including zero typed
  results on decoding failures. Final real-IdP verification remains required.

Use sqlc for queries in the affected stores as their contracts settle in C/E;
keep explicit transaction ownership and avoid a generic repository. Shared runtime
construction, lifecycle and importer removal remain D/E. No new framework,
microservices, blanket JSON normalization or full product regression is added to C.

Order: settle contracts -> integrate reviewed bounds -> registration coordinator
-> compose C3/C4/C5 and finish host policy -> focused gates and both independent
stage QA -> D -> E. Existing D-001–D-003 closure conditions remain mandatory.

`interaction.TurnCoordinator` already owns winner selection and resume-before-model behavior using `interaction.Store`. `interaction.OrderCoordinator` owns order binding/outcomes. `applicationauth.Authorizer` verifies each operation; `appclient.LocalOrders` and `Host.LocalDerived` demonstrate authenticated local execution. These remain the foundation, not competing stores or principals.

Stage C completes three changes:

1. Registration and knowledge have typed authenticated operations and scenario coordinators shared by their manual and agent paths.
2. One agent host owns context, provider invocation, read/script budgets, discovery and execution policy. Telegram owns trusted input adaptation, keyboards/cards and delivery.
3. Account mutation, active slot workflow and cross-domain projections have explicit owners. Moving files without moving these decisions is insufficient.

## Enabled consumer inventory and transport disposition

| Consumer | Current concrete boundary | C disposition and deletion proof |
| --- | --- | --- |
| Telegram registration menus/actions | `bot/pass_menu.go` calls `Client.ExecutePassBooking`; `pass_payment_media.go` submits proofs; `pass_batch.go` uses raw `Client.Call` for batches/tiers. Registration agent binding/context/reads live in `pass_agent_*.go`. | Route commands, grounded batches, reads and expected outcomes through registration contracts. Keep callback owner/token/revision parsing and card rendering in Telegram. Delete raw bot URLs only after named methods cover all these callers. |
| Registration Sobek tools | `script_pass_registry.go`, `script_pass_execute.go`, `script_privileged_reads.go`, `script_pass_discovery_privacy.go`; `appclient/pass_*`, `script_domain_reads.go`, `script_privileged_client.go`. | Agent host invokes the same registration operations. Preserve queue/history distinctions, exact payment-role scope, owner booking/invitation identity, source-bound continuation and external proof/export checks. A join/cancel-only coordinator is not complete. |
| Telegram knowledge actions | `knowledge_buttons.go`, `knowledge_actions.go`, `knowledge_execute.go`; `Client.ExecuteKnowledge`, host derived/assessment/source-link methods. | Knowledge coordinator binds observed versions, mutation keys, proposal assessment and source linking. Telegram selects a card/view and renders a typed outcome. |
| Model and script knowledge/history | `knowledge_context.go`, `memory_tools.go`, `script_knowledge.go`, `script_memory_*`, `history_reads.go`, `appclient/knowledge_client.go`, `memory_client.go`, `memory_sources_client.go`, `history_client.go`. | Typed user reads and host-only derived/assessment/history operations. Host owns bounded read reservations and context refresh; knowledge/conversation remain data and authorization owners. |
| Mini App/browser | `miniapp.Client` in `contracts.go`: order reads/quote/execute, massage timetable, food view/command/quote/legacy menu, Telegram authentication. No registration or knowledge methods are enabled in this inspected web contract. | Keep these actual browser HTTP routes. Do not invent a registration/knowledge web consumer as a reason to preserve a bot loopback, or remove food/timetable routes because C focuses elsewhere. Recheck the interface at implementation freeze. |
| Separate `zns bot` and `zns api` modes | `cmd/zns/main.go:runCommand` still enables both; the separate bot constructs an HTTP `appclient.Client`. Combined `app` injects local orders and derived operations in `cmd/zns/app.go`. | They are actual enabled network consumers. Removing the combined-app loopback does **not** authorize deleting API routes. Retain one HTTP adapter using the same application/domain operations. Any later retirement of these modes needs a distinct consumer/config decision. |
| User and host HTTP APIs | `api/pass_booking.go` and associated batch/admin/payment/takeover/export routes; `api/knowledge.go`, memory/history routes and internal host endpoints. | Preserve authenticated transport for enabled network mode and contract tests. Keep host audience/operation restrictions. A source search with no miniapp caller is not proof that a public endpoint has no external consumer. |
| Source/media/script adapters and maintenance | `assistantsource`, `scriptclient`/`scriptservice`/`scriptworker`, media adapters; runtime maintenance/domain delivery. | Helpers keep bounded computation and transport only. They receive no user/host database capability. Runtime lifecycle and delivery scheduling remain Stage D; source content is not an authorization grant. |

Before removing any transport helper, produce a method-to-caller list for bot, host, miniapp, command modes, tests and tooling. Convert every enabled caller or retain the real HTTP adapter. Do not retain an obsolete Go forwarding method merely to avoid updating its callers.

## Implementation slices and exclusive ownership

These are buildable checkpoints inside one Stage C candidate, not separate acceptance claims. Assign concrete agents to the owner labels before writing. Shared application/runtime wiring has one writer throughout.

### C1 — authenticated registration and knowledge application operations

**Owner: boundary integrator.** Own `appclient` local boundary additions, `applicationauth` only if required, `api` dependency adaptation, `runtimeapp/services.go`, `cmd/zns/app.go` and composition tests.

Extend the existing typed local boundary with explicit registration and knowledge operations. Reuse the local order credential sequence: `UserToken` per operation, delegated exchange, `Authorizer.Authorize`, owner equality, then domain call. A principal from intake is not a reusable grant. Preserve issuer/audience/actor verification and introspection, known local owner, inactive denial and retryable provider outages. Both local and HTTP paths must preserve cancellation rather than turning it into an authorization refusal.

Keep `Client` user capabilities separate from `Host`: source checks, derived mutations, history provenance classes, assessment, Telegram metadata and identity provisioning cannot be selected from user/model arguments. Local host operations need the equivalent explicit host capability and live user revalidation; removing HTTP must not remove that distinction. Reuse `LocalDerived` and existing typed domain services instead of a generic execute bus.

Add named batch/tier methods where bot currently constructs URLs. Move payload-size, authority-count and semantic validation to the application/domain entry when HTTP decoding currently supplies a required bound. Otherwise the local path could bypass it. Keep binary proof/export bounds and redirect/credential isolation on retained network adapters.

**Proof:** adapt `appclient/local_orders_auth_internal_test.go`, `host_boundary_test.go`, `derived_auth_internal_test.go`, `applicationauth/auth_test.go`; add a registration/knowledge local-versus-HTTP matrix for foreign/missing owner, wrong issuer/audience/delegated actor, inactive identity, verifier outage, cancellation and attempted user access to host-only methods. Use the real test identity provider for final acceptance. No policy shortcut based on a sandbox signer.

### C2 — registration scenario coordinator

**Owner: registration developer.** Own new registration coordinator files under `interaction`, plus an explicit agreed list from `bot/pass_agent_bind.go`, `pass_agent_context.go`, `pass_agent_reads.go`, `pass_agent_execute.go`, `pass_menu.go`, `pass_batch.go`. Do not edit shared saved-plan/source/store types independently.

Give the coordinator typed read/bind/execute/resume outcomes for solo/pair invitations, cancellation, payment admin/proof decisions, assignment/takeover, menus and batch continuation. Carry owner, event, observed version/identity, existing operation key and host derivation explicitly. The same coordinator must support manual → agent → manual transitions without rebinding an already saved command.

Retain `passbooking.Service` as transaction owner: event/actor ordering, profile changes, capacity allocation, payment participants, audit/outbox and operation receipts remain atomic. `derivedmutation` composes source and target locks; do not duplicate it in the coordinator. `pass_admin_batches.source_derivation` remains the first immutable source, checked per pending item; a manual resume cannot erase it. Current target authorization precedes receipt replay, and committed items survive source loss truthfully.

Telegram retains contact/update adaptation, button ownership, menu revision and external rendering. Return typed committed/refused/pending outcomes rather than localized strings from the coordinator. No DB transaction spans Telegram delivery.

**Proof:** bracket with `pass_agent_test.go`, `pass_menu_test.go`, `pass_registration_authorization_test.go`, `pass_batch_runtime_test.go`, `derived_pass_batch_test.go`, registration invitation/queue identity tests and affected source-revocation tests. Add only missing coordinator-level continuity/crash cases: saved bound command before commit, domain receipt before interaction result, result before Telegram completion; restart must not invoke the model or change the key/version. EN/RU stale menu and free-text continuation belong to fresh FQA.

C2 execution contract: route already-bound manual and derived commands through one
registration executor; preserve the original key and derivation. Command and
assignment metadata use the existing registration_action writer after the domain
result or authorized canonical receipt. A failed metadata write preserves the
committed result and both error causes, and must not be translated into an ordinary
domain refusal. Recovery probes the saved command without model calls or rebinding.
Batch results remain canonical per-item outcomes: a nil aggregate error does not
prove all items committed. Batch execution does not write registration_action;
its existing per-item receipts and script ledger retain outcome ownership.

### C3 — knowledge scenario coordinator

**Owner: knowledge developer.** Own new knowledge coordinator files under `interaction`, `bot/knowledge_actions.go`, `knowledge_execute.go` and the knowledge-only portion of context/buttons after C4a separates shared context assembly.

Move command grounding, durable proposal assessment sequencing and attachment of original request references out of Telegram rendering. Keep `knowledge.Command`, `Result`, typed assessment and original operation identity. Distinguish manual publication from a newly derived mutation; neither path may remove stored original provenance. Persist completion/assessment evidence before presentation so interruption cannot repeat mutation or turn a retry into a new proposal.

Knowledge remains owner of private/shared memory hierarchy, review permissions, revisions, receipts, terminal filtering and publication. Conversation remains owner of original versus derived history. Preserve `readsource` direct/causal evidence, exact event/domain grants (including empty privileged reads), owner identity and generation. Check origins at new writes and later exposure; authorized committed receipts must not become an opportunity to reveal revoked text.

The approved [author-submission contract](knowledge-publication-decision.md) is part of C3: a complete proposal stays private until its author manually confirms its immutable text, version and destination. Submission and reviewer approval are separate durable operations; model/script execution cannot supply consent. Public results and receipts expose opaque source references while complete provenance remains server-side for live ancestor retirement. Preserve this boundary when moving the coordinator, including downstream proposals and facts.

**Proof:** `knowledge_bot_test.go`, `knowledge_authorization_test.go`, `script_knowledge_test.go`, causal knowledge receipt/recovery/publication tests, memory deletion/provenance tests, `privileged_source_revocation_test.go` and `privileged_domain_source_test.go`. Verify manual review of an agent proposal, interrupted assessment/source linking, private owner isolation, A revoked/B retained, no resurrection after observed revocation, and truly original imported assistant messages unchanged.

Also retain `knowledge_submission_test.go` and `knowledge_publication_provenance_test.go`: exact author consent, concurrent retries, ordinary fact readers and private ancestor isolation must survive the C3 extraction. These are requirements for C, not a claim that the current B candidate has passed independent acceptance.

### C4 — one agent-host owner, not a second turn engine

**Deferred D-001:** Daniel explicitly permits advancing with the known d096 race deadline failure. During C4, remove duplicate quote hydration/serialization between preparation and execution while retaining current execution-time authorization. Measure the effect and rerun the original d096/escaped pair with unchanged assertions and concurrency, then the full gate. See [DEFERRED.md](../DEFERRED.md). The deferral is not a passing test result and does not cover unrelated failures.

**Owner: host integrator.** Own new `internal/agenthost` package, shared context/loop/script files and receipt storage, `bot/turn_host.go`, affected `bot.go`/AV/history adapters. Runtime wiring is changed only by the boundary integrator in an agreed patch.

**C4a, serial first:** split `knowledge_context.go`'s `reauthorizeModelContext` and `addSupportingContext` from knowledge-only reads. Define a concrete admitted host input carrying authenticated owner/update, trusted original utterance and AV references; do not pass Telegram update structs or a whole `*Bot` into the host. Keep one authoritative registration/knowledge context policy, not copied host and bot variants. This unlocks the two domain writers without colliding on the same file.

**C4b:** implement the existing `interaction.TurnHost` contract in the new host. Move plan construction and the bounded read/model loop (`createPlan`, `av_plan.go`, `history_reads.go`, settings/usage/diagnostics integration). `TurnCoordinator` still controls load/winner/terminal semantics. Preserve `BeforeProvider` refresh before each actual provider request, including selection followed by answer, and original utterance versus inspected AV content. Keep consumed-voice cleanup after validated winning state; cleanup failure never authorizes another model call.

History responses must expose bodies consistent with the returned generation. Lazy cleanup of already-old derived rows must not create a new retirement epoch, while new source retirement must invalidate the selected derived bodies before exposing the new generation. Valid history windows may aggregate multiple individually bounded authority records without inheriting a single record's limit. Preserve the corresponding generation and authority-window regressions during host extraction.

**C4c:** move script admission/reservation, live registry, dispatch and durable read/script evidence under that same host. Relevant current files are `script.go`, `script_tools.go`, `script_tools_store.go`, `script_registry.go`, `script_scope.go`, domain preparers, `script_pass_privacy.go` and source-authority assembly. Use one registry for `$list`, `$help`, visibility and known-name calls; never replace it with a startup snapshot. Keep isolated Sobek as `scriptclient` execution, not a domain service. Keep exact host-bound authority outside model JSON, operation continuation evidence and completed call receipts after worker interruption.

The existing `interaction.Store` remains the saved-turn owner. A concrete host store may own read/script reservations in `bot.interactions`; do not add another winning-plan store or a generic repository. Preserve reserve-before-I/O, exhausted/interrupted slots across restart and current bounds: two script runs, eight calls, 4 KiB final script result, existing page/authority and provider-input budgets. Native JS filtering plus per-tool help remains the discovery mechanism; no new query language is needed.

**Proof:** `interaction` winner/store tests; `agent/provider_authorization_internal_test.go`; `script_bot_test.go`, `script_tools_test.go`, privileged tool visibility/call denial, script continuation/authority tests, history deletion/summary tests and agent diagnostics tests. Add import/dependency checks that `agenthost` does not depend on Telegram transport or `bot`, and that Telegram delegates plan creation rather than retaining a second model loop. Run a real isolated-worker interrupted/resume scenario, not only fake callbacks.

### C5 — account, workflow and shared projections

**Owner: account/projection developer.** Own extraction from `core/preferences.go`, `telegram_metadata.go`, `core.go` and corresponding explicit caller changes. Boundary integrator alone edits shared runtime/API composition.

Put user preference/language operations and trusted Telegram metadata in an account service. Preserve language idempotency and same-transaction `broadcastprofile` maintenance. Bind metadata to the verified Telegram sender and monotonic update ID; the public agent cannot forge it. Legal-name/pass-profile rules remain `passes` owned, including the explicit registration/profile transaction helper exception.

Give the still-enabled slot workflow its own narrow owner and keep its behavior, tables and receipt/outbox rules. It is currently called by bot workflow context, workflow tools and API routes; it cannot be deleted merely because it began as a fixture. Change those consumers together, without alias wrappers for the old Go service.

The remaining legitimate shared contracts/projections may stay in a smaller `core` package:

| Shared responsibility | Owner and permitted scope |
| --- | --- |
| `ProblemError` and bounded cursor/page/chunk contracts | Shared application-contract owner; encoding/validation only, no domain mutation SQL. |
| `capabilities.go`, `privileged_reads.go` | Named read-only capability projection owner. Spans domain roles for discovery; never grants execution or replaces an exact source leaf. |
| Event metadata joins and broadcast recipient projections | Their named consumer/domain owns the query; document joins rather than inventing a universal event aggregate. `broadcastprofile` remains a derived transactional projection, not person truth. |

**Proof:** existing preference/language replay, trusted metadata/sender/monotonic-update and broadcast-profile tests; workflow manual/script/API regressions. Dependency review must show registration/knowledge/host no longer import an unrelated workflow service to obtain preferences or identity metadata.

## Required outcome → accountable owner → final evidence

| Required Stage C result | Owner | Acceptance evidence |
| --- | --- | --- |
| Registration typed authenticated boundary and shared interaction behavior | Registration + boundary integrator, with registration developer accountable for the full scenario | Local/HTTP identity matrix, manual-agent-manual registration and partial-batch restart, current ACL before replay, real PG locking. |
| Knowledge typed authenticated boundary and coordinator | Knowledge developer | Manual/agent review continuity, assessment restart, source-preserving publication, private memory and deletion/revocation tests. |
| Host owns context/model/discovery/script policy | Host integrator | Dependency review, one model loop/registry/store policy, pre-provider revalidation, interrupted model/worker recovery and unchanged budgets. |
| Telegram remains adaptation and presentation | Host integrator | No planning/domain orchestration in transport handlers; fresh EN/RU Telegram-like messages/buttons/edits/free-text/media flows. |
| Unrelated common-core responsibilities removed; remaining projections explicit | Account/projection developer | Named domain writers, atomic metadata/profile projection proof, active workflow parity and source dependency review. |
| Preserved final saved state, original importer semantics and shared composition | Boundary integrator | Matching schema/source manifest, buildable runtime without importer, affected imported-state smoke/reconciliation where formats changed; no original-history reclassification. |

## Execution and stop conditions

First freeze named C1 contracts, then split C4a serially. Before parallel domain
work, wire one real local registration read and command through per-operation
identity verification, retaining HTTP for the separate bot mode. Prove matching
authorization, errors and cancellation for that route. This checkpoint does not
complete C1 or C4. Then registration and knowledge can proceed in disjoint files
while the host integrator changes only agreed shared seams. Account extraction
starts after its application methods are agreed. Do not divide work by broad
`pass_*` and `script_*` globs: source authority, receipts and model context cross
them. One boundary integrator owns Client/Host, API/runtime composition, shared
interaction types and shared test fixtures; domain writers own separate
coordinator and test files.

Use accepted B as the behavior baseline. Run focused old/new checks after each meaningful slice, then full required formatting, pinned lint, native/build, SQL generation/diff and real PostgreSQL/race gates on the complete candidate. Freeze one source manifest before fresh independent Senior Code QA and source-blind Functional QA. Neither builder reruns nor B acceptance substitute for C acceptance. Post-review substantive repairs need fresh affected review.

Stop when every outcome in the table is implemented and both gates plus required checks pass. No broader orchestration framework, microservices, new concurrency scheduler, event sourcing or unified outbox is part of C. Stage D still owns lifecycle; E owns final disposable importer/removal evidence. This preparation does not increase completed percentages or claim a new deadline.

Written by registration_batch_derivation (GPT-6/Codex)
on behalf of Daniel Drizhuk

C2 closure also includes [D-002](../DEFERRED.md): replace indistinguishable
ID/tool-only registration operation discovery with bounded, currently authorized
summaries from the same coordinator/store. Prove selection among two operations
of the same kind through public tools without operator or SQL assistance. Keep
source retirement and current permission checks before revealing domain details.

## Active draft ownership

The runtime_stage_d_preflight developer exclusively owns qa.local/architecture-stage-c-draft/source for the first C1 checkpoint: authenticated local PassBooking read and ExecutePassBooking command, combined-app wiring and focused boundary tests. The frozen B source and platform remain unchanged. Other C domain writers start only after this contract is verified. Root owns composition decisions and plan/progress files. Draft source is not accepted or integrated evidence.

C2 also owns [D-003](../DEFERRED.md): receipt-bound successor validation for a batch that changes its own versioned registration source. Preserve the immutable original derivation, current grants, causal/history fences and external-write detection. Recover visible authorized effect status separately from obsolete private source payloads. Both original permission/version recovery scenarios remain unaccepted until fresh closure QA.

Current implementation ownership: `qa.local/architecture-stage-c-working/source` is the verified 1477-file successor of the C1 checkpoint. `isolated_fqa_stands` owns C4a agenthost and the exact bot context seams listed in its preparation proposal. `runtime_stage_d_preflight` owns C2 batch source evolution in derivedmutation/pass_batch.go, new narrow passbooking receipt helpers and a dedicated integration test. Root owns shared composition and plans. C1 frozen source and B QA stands stay unchanged. Two heavy jobs maximum; pinned lint is serialized. The earlier C1 draft ownership above describes the completed checkpoint, not authority to modify its frozen source.

C1 read continuation runs in parallel with those disjoint changes: `c1_registration_reads` owns only appclient/pass_booking_client.go, new local_registration_reads.go/tests and integration/local_registration_reads_test.go. Scope is the existing PassEvents, PassInvitations, PassPaymentAdmins and PassQueue methods with the same fresh authorization and HTTP fallback. No new heavy job starts until one of the two assigned slots is released.

C4a checkpoint now has passing native/vet/pinned-lint and eleven real-PG checks on the isolated `check-c4a-source` snapshot (1479 files). Fresh independent Code QA reads that frozen snapshot. The host integrator continues C4b only in the working source: agenthost, bot.go, turn_host.go, av_plan.go, history_reads.go, pass_plan_privacy.go, host_context.go and directly affected tests. Other bot paths require an explicit ownership update.

Knowledge boundary work is assigned to `c1_knowledge_boundary`: appclient knowledge/memory methods, client.go/host.go and combined-app wiring in cmd/zns/app.go; bot/memory_tools.go is its sole bot caller seam. The existing bounded reply-evidence envelope may move from API to knowledge as one shared implementation; the HTTP shape, authority-union cap, local exact item references and source-limit error mapping must survive. User operations cannot select host-only derivation, assessment or author consent. History-specific methods in shared client files stay unchanged. This work is implementation in progress, not an accepted boundary.

C5a has a separate baseline-bound development tree under `qa.local/architecture-stage-c5-account/source`, owned solely by `stage_b_fixture_builder`. It moves preferences, prepared language changes and trusted metadata from core to account and updates actual consumers without aliases. Transport-neutral account/broadcast projection values replace incidental Telegram dependencies. This tree is reconciled with the working C candidate before final composition; no concurrent C-working file ownership is granted by this assignment. Workflow and remaining shared projections remain C5b work.
