# Architectural refactoring — required main-goal scope

Added by Daniel on 2026-09-26. This extends the existing full Python-to-Go migration goal; it does not replace functional parity, removable import, identity integration or the two independent QA gates. The goal is not complete until both migration and the architectural stages below are accepted. Local commits, branches from the current branch and merges back into it are authorized. Push, PR publication and production deployment require separate authorization.

Reference: [global architecture review](architecture-review.md). Target: a modular monolith organized around application scenarios and explicit data owners, with one main CPU-only Linux/Docker application and PostgreSQL.

The [mid-C independent audit](architecture-mid-c-audit.md) adds explicit C
completion criteria for residual host policy, typed admitted-operation projection,
assessment deferral and dependency direction. The [C plan](architecture-stage-c-plan.md)
records scope and acceptance; runtime lifecycle and importer removal stay in D/E.

## Unreleased Go architecture

Daniel confirmed on 2026-09-27 that this branch has reached no production and
explicitly permits more aggressive refactoring or architectural changes.
Internal Go APIs, schema, package boundaries and saved-state formats may break
compatibility with intermediate branch snapshots. Update the forward importer
to the final model; do not build compatibility layers solely for development
snapshots. Recreate synthetic stands when that is simpler and preserves the
original source import and its verification evidence.

Preserve required Python data and user behavior, current authorization and
same-version durable retry/restart semantics. The stages below describe required
outcomes, not an obligation to retain each current adapter or perform one-for-one
file moves. Reassess boundaries for a simpler final design; independent Code QA
and Functional QA still apply to the resulting complete stage.

## Required stages and evidence

| Stage | Required result | Acceptance evidence |
| --- | --- | --- |
| A. Ownership and contracts | Assign owners for domain commands/queries, tables, transactions, interaction state, identity and importer. Preserve delegated identity and live domain authorization. | Checked ownership map covering existing modules, cross-domain writes and transaction boundaries; independent Code QA. |
| B. Composition and first vertical scenario | Combine privacy corrections, authenticated application boundary, final saved-state ownership and complete orders coordinator. Runtime constructs services; Telegram and Mini App consume independent typed contracts. | Manual, agent and HTTP paths use the same authoritative operation; identity equivalence, EN/RU GUI, stale callbacks, restart, deletion, revocation and real-PG concurrency; fresh final Code QA and Functional QA for the combined candidate. |
| C. Interaction and agent host | Extend the proven boundary to registration and knowledge. Agent host owns context, model loop, script discovery and bounded evidence; Telegram owns input/output adaptation. Remove unrelated common-core responsibilities and document remaining legitimate shared projections. | Source dependency/ownership review plus manual-agent-manual scenario tests; current ACL visibility/call denial, private memory isolation, interrupted model/script calls, durable state continuity; both fresh QA gates. |
| D. Runtime lifecycle and delivery | Explicit lifecycle ownership for intake, processing, reconciliation, notification delivery, source refresh and isolated helpers. Preserve domain outbox eligibility and transaction ownership. | Signal/crash/restart, durable intake and offset, bounded stop, uncertain delivery, helper shutdown and no overlapping instances; independent Code QA and Functional QA. |
| E. Disposable importer and shared composition | Runtime has no importer dependency. Classify temporary receipts versus permanent legacy references. Product and synthetic stands use the same composition with external adapters replaced. | Build and representative imported-state runtime checks without importer; removal rehearsal and reconciliation; contract scenarios and both independent gates. Existing migration work counts once. |

## Sequence

Daniel approved the priority on 2026-09-28: finish architecture C–E, then implement remaining missing features, then complete the full parity matrix and final real integrations. Before the refactor finishes, implement only missing behavior needed to define or verify the new boundaries. Keep the complete feature inventory and acceptance criteria; this changes sequencing, not goal scope. Required focused checks and independent QA of each changed architectural stage remain. Do not require the complete product parity matrix before every architectural step.

Architectural deferrals, including serious defects with a concrete replacement mechanism, follow [DEFERRED.md](../DEFERRED.md). D-001/d096 is explicitly approved for C4 and does not block advancement by itself. Other findings require their own recorded disposition; no failed check is relabeled as passed.

Stage A and B1 composition are accepted. The [concrete Stage B plan](architecture-stage-b-plan.md) combines privacy work and B2/B3/B4 into one final candidate. Separate acceptance of temporary clients or privacy snapshots is not a prerequisite for building the final turn/coordinator boundary. Use accepted behavior and pending verified findings as inputs, with explicit file ownership. Keep snapshots under QA immutable; develop the successor separately.

Use buildable internal steps and focused checks, then freeze each complete stage candidate for fresh independent Code QA and source-blind Functional QA. Temporary adapters do not need separate final QA cycles. Prior acceptance is baseline evidence, not proof of a new architecture. Preserve domain lock ordering, authorization policy and Python import semantics; final-format changes must retain their guarantees. Substantive fixes after review require affected fresh reviews.

Complete C and D after B is stable; E's final-schema importer work can proceed with disjoint ownership. Run integrated imported-state/synthetic acceptance and then authorized real test Telegram checks on the resulting architecture. All required stages precede goal completion; none requires production deployment.

For global Functional QA during B–E, one Functional Senior QA lead may coordinate
fresh source-blind subagents across independent scenario groups. The lead owns
stand changes, exclusive restart/fault barriers and the combined acceptance
report. Use disjoint fixtures and separate evidence paths; follow the
[global FQA coordination rules](code-quality.md#global-functional-qa-during-refactoring).
Small scoped reviews do not require a team.

## Stage C implementation checkpoints

The [concrete Stage C plan](architecture-stage-c-plan.md) inventories enabled
consumers, C1–C5 ownership, transaction boundaries and required proof. It is
preparation against candidate6, not implementation or acceptance of Stage C.

After combined Stage B is accepted with any explicit architectural deferrals recorded, extend its authenticated application and
final saved-state boundary rather than introducing another store or principal:

1. Extract registration and knowledge coordinators with their current operation
   keys, live authority checks and receipt/provenance policies intact.
2. Split shared context assembly/reauthorization out of `knowledge_context.go`
   before assigning knowledge and agent-host work to different writers.
3. Give the agent host the model loop, context reads, bounded durable read/script
   storage and one live registry for both discovery and dispatch. Keep isolated
   Sobek execution as an adapter without domain authority.
4. Move unrelated preferences and trusted Telegram metadata out of slot-booking
   ownership, preserving metadata and broadcast-profile updates in one transaction.
   The existing slot-booking workflow remains active; inventory its Python-parity
   role before considering removal. Unreleased status alone does not remove behavior.
5. Verify current tool visibility/call denial, private-memory isolation, interrupted
   work, unchanged budgets and complete durable metadata before both fresh QA gates.

Shared application/registry/store and runtime wiring need one owner; parallel domain
work starts after these seams are separated. Concrete caller/type and file-ownership
map: `qa.local/architecture-stage-c-preflight/REPORT.md`. Its prerelease client/JSON
compatibility recommendations are superseded by the unreleased-Go premise and
`qa.local/architecture-breaking-plan/REPORT.md`. These are planning evidence;
Stage C implementation and acceptance have not started.

## Stage D implementation checkpoints

The focused runtime preflight (2026-09-27) found existing durable batch intake,
replay recovery, bounded agent diagnostics, redaction and basic OTLP export.
Preserve these implementations; the remaining work is specific:

1. Put application admission before maintenance/source writers start, and hold
   it until all owned work stops. The current poller lock alone does not cover
   the full application lifetime. Prove old app/helper completion before replacement.
2. Move polling, reconciliation, notification scheduling and maintenance lifecycle
   to an explicit runtime owner without changing domain transaction/outbox rules.
3. Complete bounded backlog/age/retry/queue/cache/model-usage observations and
   asynchronous job correlation. Reuse existing telemetry and redaction; exclude
   private content and personal identifiers from metric labels.
   Retain safe, bounded error classification at operation boundaries: operation,
   phase, stable error code, retryability and transport status when available.
   Replacing every error with `operation failed` prevents distinguishing domain,
   authorization and delivery failures, as the update260 investigation showed.
   Do not solve this by logging raw errors, payloads, private memories or tokens.
   Verify that representative failures remain distinguishable after redaction.
4. Order shutdown explicitly: stop admission, drain or retain durable work,
   cancel/join owned helpers, flush telemetry within its own deadline, then close
   dependencies. Current database close precedes telemetry flush and must be reconciled.
5. Run combined CPU-only Linux signal/crash/drain/overlapping-start and collector
   failure scenarios on the resulting composition, then both independent QA gates.
   Include loss of the dedicated admission/lock session while an update or
   reconciliation is still running: owned writers must stop before replacement
   work starts, and saved work must recover without duplicate domain effects.
   A reconnect alone is not proof of retained exclusive ownership. Keep this
   runtime requirement separate from artificial idle eviction in a QA proxy.
6. During finalization of D, commission a fresh independent architecture audit,
   analogous to the B-to-C audit. Review the actual combined implementation and
   original requirements: ownership/dependency direction, lifecycle and process
   replacement, durable intake, ordered queues, throttling, partial batch failure,
   cancellation/recovery, extensibility and unnecessary complexity. Examine the
   remaining E/importer boundary and readiness for final acceptance. Run the
   audit alongside available final checks; it does not replace Code QA or
   Functional QA. Root triages evidence-backed recommendations into release
   blockers, scoped fixes or explicit later work, and updates PROGRESS/estimates.
   Do not turn optional redesign or the deferred UX audit into a release gate.

The source preflight is planning evidence, not a reproduced corruption incident
or Functional QA pass. Details: `qa.local/runtime-stage-d-preflight/report.md`.

### Durable throttling and partial batch failure (required in D)

Daniel confirmed this requirement on 2026-09-28. Delivery must slow down under
Telegram flood control without losing the workflow. One unavailable recipient
must not terminate an independent-recipient broadcast or unrelated batch items.

- Reuse existing durable delivery/outbox records. Keep scheduling and transport
  policy shared across broadcasts, notifications and ordinary queued replies;
  keep eligibility, consent and transaction decisions in their domain owners.
- Respect Telegram `retry_after` as a not-before deadline. Persist pending work
  and cooldown through restart; release worker/DB resources while waiting.
  Repeated valid cooldowns must not consume the ordinary failure budget and
  silently turn a delivery into a terminal failure. Invalid or unrepresentable
  delays must be visible and parked, never retried early or overflowed.
- Apply conservative configurable bot-wide pacing and per-chat pacing. The
  response does not identify a rate-limit scope: do not infer a chat-only limit
  from the presence of chat_id. Define and test a conservative fallback for an
  ambiguous 429. Preserve required order within a conversation and avoid bulk
  work starving interactive replies. Do not enable paid broadcasts implicitly.
- Classify each item as queued/deferred, successful, permanently rejected,
  cancelled or delivery-uncertain. An explicit blocked/unreachable recipient
  ends that item, not the whole job. An opt-out cancels eligible pending items;
  recheck current consent/authorization before deferred work resumes.
- Separate definite rejection from a lost response after a possible send. Do
  not claim exactly-once Telegram delivery or blindly resend an uncertain send.
  Preserve its visible outcome and recovery policy. Shared credential/config
  failures pause the affected service rather than being misreported as a batch
  of bad recipients.
- Independent batch actions retain per-item progress and stable operation keys,
  resume only remaining/retryable items and return an aggregate partial result.
  Preserve explicit transactional/all-or-nothing domain operations: this rule
  does not authorize continuing their dependent steps after a prerequisite fails.
- Expose bounded queue age, cooldown/next attempt, retry classification and
  per-job success/deferred/rejected/uncertain counts without private payloads.

Existing baseline: `telegram.APIError` exposes RetryAfter; admin-message delivery
persists per-item retry scheduling and announcement delivery recognizes 429.
The current admin-message adapter stops 429 retries after three attempts and
the adapters use different delay bounds. These are reuse points and gaps, not
proof of the shared scheduling contract above.

Acceptance: synthetic Telegram returns per-chat and bot-wide throttling,
repeated 429, permanent recipient denial, a lost response after acceptance and
mixed outcomes. Check other eligible recipients continue when permitted;
restart during cooldown and mid-batch preserves deadlines/order/completed items;
consent withdrawal prevents a deferred send; cancellation and shutdown retain
pending work. Use one shared stand for compatible cases and exclusive fault
lanes only where shared process state changes. Fresh Code QA and black-box FQA
must observe actual public behavior and wire call times.

Protocol references: [ResponseParameters](https://core.telegram.org/bots/api#responseparameters)
and [Telegram sending limits](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this).

### Registration admission order and ordered announcements (required in C/D)

Daniel specified first-initiating-request order on 2026-09-28, including promo
opening bursts and repeated registration buttons/commands. This is a domain
ordering contract, not just a transport rate limiter.

Clarification: initiation means the first request that unambiguously starts
registration for a specific event and reaches that event's sales-open check.
Opening a generic registration menu, browsing or listing events does not reserve
priority. Selecting an event counts only when that action starts registration
and performs the sales-open check. A direct event-specific command or callback
uses the same boundary. Do not backdate priority to an earlier generic request.

- Preserve a canonical ingress position before asynchronous processing, model
  work or multi-step forms can reorder requests. At the event-specific boundary
  above, link the first registration intent to its initiating request's position
  for that event and participant. Generic navigation creates no queue entry.
  Do not use worker
  completion time, booking-row insertion time, Telegram user ID or client clocks
  as the priority. Define deterministic admission across Telegram intake and
  other supported entry points; preserve original update identity on replay.
- Repeated initiation clicks and later form steps reuse the existing intent and
  priority. They neither create extra queue entries nor move the participant
  ahead or behind. Concurrent workers, retries, recovery and cancellation must
  have explicit fenced state transitions. Do not conflate replay with a new
  registration after an explicit cancellation.
- Attempts before opening must not acquire a pass before sales allow it. Preserve
  their original initiation order where they are retained as queued intents;
  the queue cannot be reordered by repeated pressing around the opening time.
  The agreed retention is configurable, defaulting to 10 minutes. When an
  unfinished turn expires, move that intent to the queue tail and begin a new
  bounded turn; preserve the registration draft, entered data and original
  ingress identity. This is not cancellation. Repeated clicks do not renew the
  deadline or restore an earlier rank. Persist the effective order and deadline
  so restart and concurrent allocation preserve the same decision. Existing role, pair, tier and capacity eligibility rules
  remain explicit constraints; no silent change to those allocation rules.
- Allocation and pending announcement release use the same effective queue rank.
  Moving an unfinished intent to the tail must release eligible later completed
  registrations without cancelling the moved draft. Keep original ingress for
  audit; do not let replay restore an expired rank.
- Carry the same registration ordering identity into the hype-chat outbox when
  registration is confirmed. Do not announce incomplete/failed registrations.
  Within each ordered destination lane (bot, chat and topic as applicable), only
  the head may send. A cooldown, in-flight send or uncertain result must not let
  a later announcement overtake it. Resolve uncertain delivery explicitly before
  advancing that lane. Other independent lanes may continue.
- Permanent failures advance an ordered lane only after a durable terminal
  outcome. A skipped head and the reason remain visible. SQL `ORDER BY id` with
  `SKIP LOCKED` or filtering only currently due rows is not sufficient: locked,
  deferred or in-flight heads must not be bypassed within the same lane.
- Preserve ingress priority, deduplication identity and outbox lane sequence
  across schema migration and restart. Imported historical records need a
  deterministic documented fallback; never invent original ingress evidence.

Baseline gaps found by source inspection: passbooking orders by CreatedAt and
TelegramID, with CreatedAt assigned at mutation execution; registration
announcements select due rows by id with SKIP LOCKED. These do not prove the
first-initiation and no-overtaking requirements. Implement the intent/sequence
contract in registration coordination and its schema; complete ordered delivery
and runtime scheduling in D. Final acceptance must cover both together.

Acceptance: a synthetic opening-time burst with many interleaved users, repeated
initiation and form callbacks; deliberately reverse worker/model completion
order; equal timestamps, restart after intake/before allocation and mid-send;
cooldown on the first announcement while later items are ready; concurrent
senders and other chats; terminal rejection and uncertain response at the head.
Assert persisted priority and actual wire delivery order, plus no duplicate
allocation/announcement from replay. Reuse a suite stack and isolated fixtures;
serialize exclusive crash/fault scenarios.

## Boundaries

- Preserve typed domain operations, durable state, version checks, idempotency, live authorization, and private-data boundaries.
- Prefer typed in-process application calls where they remove loopback/client complexity. Before removing the old boundary, prove equivalent trusted admission, principal-owner binding, per-operation delegated exchange and verification/introspection (issuer, audience and actor), known local identity and live domain authorization. Preserve inactive-user denial before private intake and retryable provider outages. Models cannot construct authority or access host-only delivery/provenance/metadata/provisioning operations.
- Keep HTTP for browser and genuine external consumers. Inventory those consumers before deleting routes or transport helpers. Remaining network adapters retain credential isolation and bounded validation. Removing exchange/introspection is a separate identity-policy decision, not implied by removing an HTTP socket.
- Use one explicit final saved-state format with required authority/provenance and final-format restart/replay tests. No aliases, forwarders or old-format upgrades solely for prerelease Go snapshots. Typed original-input, derived-output and trusted-outcome history operations replace optional provenance inference; derived writes retain atomic deletion fences. Forward import targets this final model and preserves Python source identity, references and receipts.
- Keep one PostgreSQL and one main application process; retain isolated non-authoritative script/media helpers.
- No generic Execute(any), generic repository, command bus, workflow framework, event sourcing or microservice split is required.
- Runtime ownership must be explicit. New cross-conversation parallelism is optional and requires measured need and separate ordering/fairness proof; it is not a prerequisite to this refactoring.
- Domain-specific payment and outbox semantics may remain different. Move responsibility, not just files.

## Budget and status

Updated 2026-09-28: remaining structural effort is 11–21 engineering days including affected QA; total remaining main-goal work is 26–53 days. See the [readiness estimate](readiness-estimate.md) for the breakdown and uncertainty. These are workload estimates, not calendar promises. Architecture is approximately 55–60% implemented and 15% accepted; C is approximately 75–80% implemented, with full stage acceptance pending. A and B1 are accepted, and B1 remains the main 1222-file composition. B2–B4 retain residual acceptance conditions carried into C. Reviewed C slices, helper lifecycle and neutral appservices are integrated in the separate development composition. Full C integration and both independent stage QA gates, D runtime ownership and E importer removal remain open. Scoped checks do not increase stage acceptance.
Current authorization (2026-09-27): Daniel explicitly reaffirmed local commits, branches from the current branch and merges back into it. This supersedes the earlier goal's no-commit clause. Push, publication and production deployment remain unauthorized. Preserve unrelated uncommitted work and frozen QA evidence; commit explicit reviewed paths only.

### Early isolated D slice, 28 September

The helper lifecycle slice has passed scoped source review and is present in the development composition. The neutral concrete appservices bundle has also passed scoped gates and fresh independent Code QA and is integrated in the development composition; runtime construction no longer needs to name the service bundle through the HTTP API package. Preserve existing values, domain transactions, local authentication, and standalone API mode. This independent extraction may proceed before the full C freeze; root integrates only its verified owned delta. Complete runtime construction, admission/fencing, drain and deployment proof remain later D work. No new framework or acceptance waiver is introduced.
