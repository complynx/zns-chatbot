# Full Go migration: staged acceptance

Branch: `feature/go-platform-sandbox`. Full parity is the goal; completing stage 1
does not mean the old bot can be retired. Each stage ends in a fresh independent
diff review, validated fixes, repeated tests and a new review if code changed.

## Pre-production architecture compatibility

Daniel clarified on 2026-09-27 that no Go changes from this branch have reached
any production. Breaking changes to the new internal architecture, APIs, schema
and serialized Go state are allowed. Forward migrators must target the resulting
model and preserve the required Python source data and agreed behavior.

Intermediate Go snapshots are not a compatibility contract. Do not retain aliases,
duplicate adapters or cache/plan upgrade paths solely for those snapshots. Local
synthetic stands may be recreated from their reproducible source import instead
of upgrading every development format. Previously accepted snapshots remain
evidence of behavior, not a requirement to preserve their implementation.

This does not relax live authorization, private-data boundaries, durable effects,
or retry/restart behavior within the final application. Preserve actual legacy
data, supported user flows and approved external contracts; update importer
mapping, reconciliation and independent acceptance alongside architecture changes.
Production cutover remains a separately authorized action.

## Mandatory local review loop

Quality is an acceptance condition for each stage, including fixes to prior stages.
Daniel approved architectural deferrals on 2026-09-28, including substantive
findings when a concrete target change addresses their established cause.
Record the cause, impact, target stage and closure checks in
[DEFERRED.md](../DEFERRED.md) before advancing with such a finding. Keep failed
checks and acceptance limitations explicit; deferral is not a pass or removal
from the full migration goal. Other substantive findings remain blocking.

The approved sequence is architectural stages C–E, remaining features, full
parity acceptance, then final real integrations. Before C–E completes, add only
behavior needed to define or verify the new boundaries. Each changed stage still
requires focused preservation checks and both independent QA gates; the complete
product matrix is not a prerequisite for every architectural step.

1. Run formatting, GolangCI-Lint (Golden config plus the Nebius Go SDK enabled
   linters), ESLint and Prettier. The commands must fail on findings.
2. Run focused unit and real PostgreSQL integration tests. Use testify `require`
   for prerequisites and `assert` for independent assertions. Run race detection
   for shared-state changes and browser checks for UI changes.
3. Fresh independent Code QA: inspect code structure, boundaries, transaction and
   error handling, test assertions, dependencies and linter suppressions. No
   implementation history or previous reviewer conclusions are supplied.
4. Fresh independent Senior functional QA: receive user requirements and sandbox
   access, not implementation details. Exercise manual and mixed agent scenarios,
   permissions, stale interfaces, persistence and failures relevant to the stage.
   QA may request improvements or additional stand types, or implement its own
   stands, fixtures and tests in assigned test paths. This does not grant product
   code ownership or relax independent acceptance. Record stand gaps as gaps;
   resolve required capabilities before accepting the affected scenarios.
5. Reproduce findings, fix real issues, rerun affected checks and request fresh
   independent reviews after substantive changes. Record evidence and remaining
   limitations. Keep the staged diff unchanged.

Advance autonomously when both reviews and the quality/test gates pass. This
does not authorize a production cutover, push or publication. Local commits and
branches/merges are separately authorized by Daniel's latest instruction.

At least one required stand must reproduce Telegram interactions through a usable
chat UI: messages, inline buttons and reply keyboards, callback acknowledgements,
message edits, file uploads/downloads and Web App entry points where used by the
ported features. Manual actions and agent actions must share the same visible
conversation, including stale buttons and updates to earlier messages. Follow
Telegram behavior and API constraints; matching its visual design is unnecessary.
Functional QA must exercise this UI. HTTP-only checks cannot replace this gate.
Additional API, fault-injection or identity stands may supplement it. Assign
separate paths and ports to QA-owned stands; coordinate shared sandbox mutations
and rebuilds so they cannot invalidate another reviewer's evidence.

## Stage 1 — runnable infrastructure and shared interactions

Configuration acceptance remains a required task: typed YAML for Go with
`defaults → YAML → environment` precedence, `ZNS_` prefix and `__` for nested
fields. Validate types and required settings at startup; test precedence and
invalid configurations. A secret-free example, implementation and focused checks
exist. The complete Functional QA gate remains open. See PROGRESS.md for current
evidence.

- Go Core API + separate bot process; PostgreSQL with separate schema roles.
- Manual GUI and model proposals use the same action executor and renderer.
- Scoped interaction history, persistent workflow, card edits and fallback sends.
- Fake Telegram, deterministic model, local browser UI, fault injection.
- OpenAI Responses adapter with `gpt-6-luna`; optional sandbox egress profile.
- Transactional capacity, replay protection, stale callbacks, permission checks.
- CI, Dependabot and test instructions.

This stage implements a generic booking fixture, not the production massage or
shuttle algorithms. Authentication uses explicit fixed sandbox identities.

## Cross-cutting requirements — observability and shutdown

Required backlog; implementation and acceptance remain pending.

- Production runs Linux containers on CPU-only hardware, without GPU, NPU or
  TPU. Required application, media and retrieval paths must work in this
  environment. Optional embedding acceleration cannot be a deployment requirement.
  The deployment configuration uses a pgvector PostgreSQL image; installed
  extension versions, indexes and embedding generation still need verification.

- Deployment constraint for the first version: one main application process on
  one machine, one active instance. Keep bot/API/agent module and authorization
  boundaries, but do not require separate service deployments. This supersedes
  the earlier separate-process deployment target; current sandbox wiring still
  needs consolidation. Networkless media decoders remain isolated helper
  processes, with bounded lifetimes and no independent business-event ownership.
  PostgreSQL and telemetry collectors remain supporting infrastructure.
- Restart by stopping the old instance completely before starting its replacement.
  The launcher/service manager must enforce that sequence, including after a drain
  timeout. No rolling overlap, distributed leases or leader election are required.
  Use in-process coordination where sufficient; retain database transactions,
  durable intake and idempotency for concurrent handlers and crash recovery.
  Test a second launch is prevented until the old instance exits, including its
  owned processing helpers. Multi-machine or active-active operation is out of scope.
- Use structured `slog` with context propagation and trace/debug/info/warning/error
  levels (define an explicit trace level below debug). Carry request, update, job
  and trace identifiers across bot, API, agent and worker boundaries. Compare
  event selection and personal-data visibility with the Python implementation;
  record the intended level for operational events, expected refusals and failures.
- Centralize redaction, including nested attributes and upstream error messages.
  Never log usable credentials, authorization headers, API keys or JWT signatures.
  A diagnostic JWT may retain safe portions with its signature replaced by stars;
  its payload can contain personal data and needs the same filtering as other
  fields. Most personal data belongs only in explicitly enabled debug/trace logs;
  allow selected necessary info fields based on the Python behavior and review.
  Debug does not disable secret redaction. Avoid routine raw prompts, media and
  document contents. Test redaction at every level and through error paths.
- Inspect the existing Alloy configuration in the deployment/server-configs repo
  before choosing export protocols, labels and endpoints. Match the user's
  Grafana Labs pipeline. Include metrics, distributed traces and log correlation;
  use bounded-cardinality labels, never user IDs, personal data or secrets as
  metric labels. Cover Telegram intake/backlog/lag, outcomes/retries/delivery,
  API latency/errors, database pools/queries, agent/model latency and usage,
  media workers, cache and queue behavior. Trace asynchronous processing as well
  as synchronous requests; use sampling and bounded telemetry buffers.
- Provide OpenTelemetry tracing as a platform capability even when the deployed
  Alloy has no trace pipeline. Expose typed YAML/env settings for enabling export,
  OTLP endpoint and sampling, with validated defaults and credential redaction.
  Instrument service boundaries and propagate trace context; correlate logs with
  trace/span IDs when present. Export can be disabled without changing business
  code or preventing startup. Prove both disabled operation and enabled OTLP span
  delivery on the local collector stand; production collector setup is a separate
  deployment task, not a reason to omit platform tracing.
- Provide a local collector stand and synthetic end-to-end checks proving metric
  collection, trace propagation, log correlation and redaction. Verify exporter
  failure cannot block business processing; document dashboards and actionable
  alerts. Local deployment configuration inspected at
  `server_configs/configs/alloy/config.alloy`: Prometheus remote write, cAdvisor
  and Traefik scraping, Docker log collection into Loki are configured. No OTLP
  receiver or trace pipeline was found in that file. Add application scraping and
  a trace receiver/export path as needed; confirm the live deployed configuration
  before rollout. Production delivery is not yet verified. Do not copy deployment
  credentials into this repository or reports.
- Persist every accepted Telegram event in PostgreSQL before acknowledging it or
  advancing the polling offset, including the full batch before acknowledging
  the batch. Durability is part of normal intake, not a shutdown-only action.
  Retain the payload/references and processing state required to resume; use a
  unique update ID for intake deduplication. On startup, recover unfinished work
  before or alongside new intake with explicit ordering for each conversation.
  A hard crash must not require a signal handler to preserve accepted events.
- On termination signals, stop accepting new work, then finish the current
  Telegram batch or durably retain unfinished events for restart. Advance polling
  offsets/acknowledgements only after receipt is durable;
  cover every event in a partially processed batch. Bound the drain period;
  preserve recoverable work when it expires. Coordinate API requests, worker jobs,
  queue leases and outbound deliveries. Keep idempotent effects across replay and
  handle uncertain delivery outcomes without claiming exactly-once transport.
  Flush telemetry within a separate bounded deadline before closing dependencies.
- Test signals during batch receipt, domain commit and delivery, drain timeout,
  restart and forced interruption. Prove no acknowledged events disappear,
  durable business mutations are not repeated, and unfinished work resumes.
  Both independent QA gates must cover the observability and shutdown scope.

## Stage 2 — orders and payments

Source: `zns-chatbot/plugins/orders.py`, `food.py`, `zns-chatbot/payment_methods.py`,
`static/orders_service.mjs`, `static/orders.mjs`, existing order tests.

Port catalog validation, authoritative totals, removed historical menu items,
event scoping, meals/extras, mutually exclusive tours, shuttle/tour capacity,
proof uploads, cash requests, country/payment administrator selection, acceptance,
rejection, attempt tokens, cancellation, reconciliation, priority by proof time,
reminders and exports. Existing tests around stale attempts and seat displacement
are explicit parity requirements. Preserve active web ordering capabilities.
Legacy `/food` and `/activities` entry commands are commented out in Python;
historical data/export compatibility still needs a decision based on production
usage. `/exportfoodorders` remains registered. Do not equate a generic booking
fixture with this module's completion.

## Stage 3 — passes, massage, operations and remaining interfaces

Source: `passes.py`, `massage.py`, `superuser.py`, `events.py`, `server.py`,
localization resources, lineup and tests.

Port paired/distributed waitlist assignment, balance gates, couples, tiers,
promos/date blocks, concurrency limits, legal name/passport/role collection,
payment lifecycle, exports and administrator reassignment/cancellation. Preserve
`/passes`, `/passes_assign`, `/passes_cancel`, `/passes_tier`,
`/passes_switch_to_me`, `/passes_uncouple`, `/passes_table`, `/legal_name`,
`/passport`, `/role` behavior with explicit permissions.

Port massage durations/prices, parties/timezone, specialist work slots, shared
table capacity, daily client limit, instant booking, specialist/client views,
notification preferences, reminders and timetable. Include `/user_echo`,
`/get_file`, `/send_message_to`, `/refresh_events`, recipient selection, templates,
forwarding and delivery audit. Sandbox destinations stay fake even for admin
flows. Port RU/EN interfaces, web authentication and upload/download boundaries.

Avatar processing stays outside the Go runtime and is not deployed, as requested.
Keep relevant historical metadata and a future typed external job contract.

## Stage 4 — production identity, knowledge and removable import

Replace the sandbox identity adapter with verified Zitadel issuer/audience/actor,
mapped Telegram identities and real-user authorization. Integration tests use a
separate test Zitadel instance. A durable verified external identity mapping is
authoritative; synthetic email must not adopt or link an existing account. Resolve administrator-account
impersonation restrictions without granting broad IAM_ADMIN authority to the bot.

Use the stage-1 OpenAI `gpt-6-luna` adapter; port knowledge/document refresh, lineup
and localized context through the model port. Model outputs remain proposals.
REST is the initial contract; MCP remains an optional adapter, not a completion
dependency unless a concrete client requires it.

Quota contract updated26September: replace question-count limits with accounting
for all paid agent and other provider operations. Use one OpenAI credit as the
canonical credit unit of the monetary API balance, with explicit conversion for other providers. Preserve
original usage and versioned conversion evidence. Support configurable user limits
and unlimited users (superadmin by default); unlimited still records costs and
does not bypass authorization or execution safety bounds. Unknown usage is not
zero cost. Replays must not duplicate accounting, while genuinely repeated paid
provider requests remain visible. Codex calls are limited to test stands; when
comparable usage figures are unavailable, a clearly labelled approximation is
allowed there. Keep estimated, reported and unknown costs distinct. Allocation
defaults must be explicit before enforcing the replacement limits. Historical
question counts must not be converted into invented historical costs.

Implement a separate `tools/migrate` Go module with Mongo dependencies isolated
from runtime builds. Snapshot manifest → staging → resumable identity provisioning
→ dependency-ordered import → reconciliation → single-writer cutover. No reverse
migration. Source snapshots and receipts are archived, not used to overwrite newer
production writes. Only after all parity gates pass, remove importer, credentials,
temporary schema and importer CI; retain core identities/legacy references and
normal SQL migration history. Never import personal production data into sandbox.

The bounded users importer runs `zns-migrate apply users --stage <directory>
--plan <users.jsonl> --resolutions <private.json>` with `MIGRATE_DATABASE_URL` in
the environment. It regenerates the plan from the verified stage and requires
identical bytes before opening PostgreSQL. The private resolutions object has
`version: 1`, `plan_sha256`, `identity_attested: true`, and `users`, each containing
`legacy_key`, `owner`, `issuer`, `subject`, and an explicit boolean `can_book`.
The operator must verify each HTTPS issuer/subject link independently: attestation
is not live Zitadel verification, provisioning, email matching, or an administrator
grant. Duplicate source keys, owners, identities and JSON keys fail closed. Only
identity and booking-policy blockers can be resolved here; all other unmapped or
invalid data prevents the whole preflight from reaching SQL.

Stop runtime writers for the import and reconciliation window. The one-off users
command commits all users, profiles, identity links, legacy references and receipts
in one domain transaction. Events, orders and messages also commit whole domains;
food, passes and massage already use whole-domain transactions. There is no
per-record partial resume. Existing target accounts remain conflicts even if
their values match. Explicit `reconcile users` verifies the same plan/resolution
bytes and original target fields without updates. Changed target state fails
reconciliation; there is no repair or overwrite mode.

On failure, stop and restore or recreate the complete pre-import target database,
then reapply all domains in dependency order using the same verified inputs.
Do not infer a safe prefix from a failed response or delete selected core rows;
the importer never automatically erases the database. Keep an explicit mapping
and archived private inputs for this recovery. See [one-off import](import-oneoff.md)
for command and recovery boundaries. Migration041 retains source-key/record hashes
and owner references in Core after importer removal. The temporary
`migrate_import.user_receipts` schema remains until import/reconcile and
whole-database recovery acceptance are complete.
CLI output contains counts, hashes and stable error codes, never private input or
database connection details. Acceptance for this slice uses synthetic data only;
it does not establish production cutover or other-domain import parity.

Receipt import must resolve legacy Telegram file references to immutable bytes
and create owner-bound Core proof IDs before importing the linked payment state.
Record unavailable files in the reconciliation report; never substitute an empty
file or claim that a missing receipt was validated by the importer. Keep source
message/file references in the import manifest, outside the runtime dependency.

## Confirmed onboarding and audience contracts

New users are provisioned automatically on their first trusted Telegram contact; prior browser login is not required. Bot-first and authorizer-first flows must converge on the same Zitadel user. Keep provisioning resumable, separate its credentials from impersonation, and never merge accounts based only on a synthetic email or fabricate Telegram OIDC subjects. Existing exact bindings and disabled/conflicting identities remain authoritative.

Broadcast templates use Go text/template and html/template with documented syntax and escaping. Basic audience filters are sufficient; the agent may inspect authorized paged data and perform richer filtering in JS before a preview and human send confirmation. Unrestricted MongoDB selectors and executable Python templates are not required.

## Required architectural stage

On 2026-09-26 Daniel added the full [architectural refactoring plan](architecture-refactor-plan.md) to the main goal. Complete ownership, runtime composition, application interactions and agent host, lifecycle/delivery boundaries, and disposable importer proof before declaring the goal complete. Preserve behavior and obtain both independent QA gates for structural stages. Production deployment remains separately authorized.

## Completion gate

Mandatory i18n: at least English and Russian across Telegram commands/help,
buttons, cards, Mini App, validation/errors, payment instructions, reminders,
administrator flows and agent replies. Keep stable API error codes; localize
their presentation. Use shared message keys, locale-aware plurals/dates/money,
Telegram language as the initial preference, explicit user language selection
and an explicit compatible-language fallback chain ending in English. Belarusian
`be` (legacy alias `by`) and Ukrainian `uk` (`ua`) fall back to `ru`, then `en`;
Polish `pl` falls back to `en`. Prefer an exact available translation before the
chain. Do not infer that every Slavic language should use Russian.
Persist the preference for UI catalogs. Agent replies follow the current question's
language, independently of the stored preference and previous messages. Verify
language switches with the real model. Treat en/ru
as initial catalog registrations, not a closed language enum; adding a locale
must not change business logic. Missing translations use the declared chain at runtime
and fail catalog-completeness checks in CI. Use CLDR-backed plural forms.
New interfaces must use catalogs, not grow hard-coded UI strings. Existing Go
strings need a dedicated RU/EN conversion before their stage is complete.
Both locales are acceptance requirements: missing-key checks and Functional QA
manual/agent/button flows, including fallback and changed-language refresh of
existing messages. The current Russian bot slice is not i18n-complete.

No production switch until active command/web capabilities have parity fixtures,
critical domain invariants pass real PostgreSQL tests, identity tests pass with
real test Zitadel, the manual mixed-mode scenarios work, the data reconciliation
report is clean and the final independent review has no substantive findings.
Scope differences must be recorded rather than labeled complete.

Current authorization (2026-09-27): Daniel explicitly reaffirmed local commits, branches from the current branch and merges back into it. This supersedes the earlier goal's no-commit clause. Push, publication and production deployment remain unauthorized. Preserve unrelated uncommitted work and frozen QA evidence; commit explicit reviewed paths only.
