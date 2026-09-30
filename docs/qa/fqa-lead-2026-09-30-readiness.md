# Functional QA stand allocation and readiness

Recorded 2026-09-30. Lead: `/root/fqa_lead`. Requirements: AGENTS.md,
docs/go-migration.md, docs/architecture-refactor-plan.md and the global FQA
coordination section in docs/code-quality.md. Full migration and Python data
preservation remain the goal. C–E acceptance precedes remaining parity and final
real integrations. No production, push or publication is authorized here.

## Current preparation epoch — 2026-10-01

Root supplied reviewed integration source commit
`ce427ca56ff9f127eba55321047c64a5e9204683`, including reviewed catalog,
agent-fixture and stand seam changes. Engineer has sole preparation ownership
for flows/recovery source export, immutable image build, isolation preflights
and managed start. Import preparation follows separately. The source epoch is
available; actual images, container handles, UI operation and freeze are still
pending lead verification. No runtime verdict follows from source review.

The actual control contract is `docs/sandbox/fqa-stands/provider-controls.md`.
Its current synthetic capability selects an exact message edit by chat/message
ID and replacement text hash, holds before application or loses a response
after application, and exposes a bounded journal. This does not establish
general retry, lifecycle, database or importer controls. Each required case
is mapped only after its actual running capabilities are observed.

Engineer preparation evidence received (not independently runtime-verified):
exact commit archive contains 2,742 files; six current Linux static binaries
were built. App image reported as
`sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`.
Helper/coordinator image builds continue. Reported native engineering checks:
1,522 PASS, 0 FAIL, 99 SKIP because PostgreSQL/unavailable checks were not
executed. These are build evidence, not Functional QA or full baseline proof.
Lead's read-only Docker inventory found no allocated FQA containers at this
preparation point. Actual resource handles and UI verification remain pending.

Subsequent lead read-only actual container inspection:

- `synthetic-qa-zns-fqa-flows-prerequisites-fake-1` and recovery counterpart
  are running on loopback 58403/58404 and 58413/58414. `Config.Image` is
  `synthetic-qa-zns-fqa-app@sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`;
  actual ImageID matches this digest.
- Flows/recovery fake config mounts respectively use
  `synthetic-qa-zns-fqa-flows-config` and
  `synthetic-qa-zns-fqa-recovery-config`. Their PostgreSQL containers use
  distinct `*-flows-pgdata` / `*-recovery-pgdata` volumes and the recorded
  `postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24` reference/ImageID.
- PostgreSQL host ports currently have no binding in actual inspection. Allocated
  58401/58411 are not verified endpoints; engineer clarification pending.
- `synthetic-qa-zns-fqa-flows-owner-coordinator-1` and recovery counterpart
  use `synthetic-qa-zns-fqa-coordinator@sha256:3e4c539a607cacbcdf8cf2bb84c07de9d8dc1f118a7f43ca91f542dc7c867c3c`
  with matching ImageID, disjoint config/state mounts, but both were restarting.
  No main app runtime was running at this inspection point. Freeze remains blocked
  on stable managed startup and usable UI/control verification.

Full image-binding record:
`qa.local/go-resume-20260929/fqa-epoch-ce427ca5/image-bindings.json`.

Latest actual lead verification: all six managed components on each stand are
running. App/evaluator/media-decoder report healthy; both coordinators are
running. Every inspected component's `Config.Image` contains the expected
`repository@sha256` reference and its ImageID/RepoDigests agree. App and PG stay
on the internal network; nominal app/PG host endpoints remain unavailable.

Lead used existing Playwright with an ephemeral headless Edge context, not a
personal browser profile. Actual DOM and screenshot evidence is saved under
`qa.local/fqa-lead-browser/`. In an exclusive preparation window agreed with
engineer, both UIs displayed `/start`, service inline buttons, updated the visible
card to draft/version 1 on selection, recorded one edit and accepted a synthetic
`readiness.txt` upload. The later media-intent card was visible. These establish
readiness capabilities, not independent product acceptance. Initial browser
wait incorrectly used an edit-count change for `/start`; corrected observation
captured the actual sent card without replaying that flows input.

The advertised deterministic agent setup returned installed/update 5, but its
completed public readback on both stands was `accepted: 0`, `rejected: 1`,
`last_status: input_mismatch`; UI displayed agent unavailable. Engineer owns
clarification of the preparation contract. Successful agent readiness is not
claimed. Provider state/arm controls and callback acknowledgement remain to be
verified in their scoped cases. No frozen reviewer epoch had begun at that point.

## Frozen scoped handoff — 2026-10-01

Engineer explicitly released flows/recovery preparation ownership: no mutation,
build or process operation remains active; images/configuration stable. Lead
freezes epoch `ce427ca56ff9f127eba55321047c64a5e9204683` for independent
manual/UI subsets. Public access/scope:
`docs/qa/fqa-lead-2026-10-01-public-handoff.md`. Reviewer A owns flows and B
owns recovery scenario writes. No rebuild/restart/reseed permitted during this
batch; engineering recipe investigation is read-only. Import remains unprepared.
Agent setup, lifecycle/SQL fault controls and import are outside current
executable scope but retained as open requirements in the 40-case matrix.
The usage merge after ce427 belongs to a later released/frozen image batch.

Both independent reviewers have started actual scoped execution on this frozen
epoch. A resumed first; B's restore briefly hit the thread limit, then succeeded
after a slot freed. No acceptance result is yet available. Public access now
includes optional fixture capability setup with explicit chosen-locale matching
and automatic enqueue. Reviewers establish the actual outcome independently;
engineer recipe clarification involved no post-freeze runtime/data writes.

## Batch closed and resources released — 2026-10-01

Both independent reports are complete. A released flows with no active browser
or process. B released recovery after its one exact before-apply hold/release
completed; no held request remains. Lead preserves both actual ce427 image/config
and evidence state. Combined acceptance/backlog:
`docs/qa/fqa-lead-2026-10-01-combined.md`. No whole original row or C/D/E stage
is accepted; independent bounded checks are recorded separately from blocked
requirements. Engineer now has bounded next planning ownership, with root gate
and next epoch confirmation required before code/image/stand handoff.

## Allocation and ownership

| Stand | Ports: PostgreSQL / app / Telegram fixture / controls | Execution owner | Scope |
| --- | --- | --- | --- |
| synthetic-qa-zns-fqa-flows | 58401 / 58402 / 58403 / 58404 | Fresh reviewer A `/root/fqa_lead/fqa_flows` | C/B residual user flows, EN/RU, manual-agent-manual, registration, knowledge, permissions, stale cards, privacy |
| synthetic-qa-zns-fqa-recovery | 58411 / 58412 / 58413 / 58414 | Fresh reviewer B `/root/fqa_lead/fqa_recovery_import` | D faults, retries, concurrency, durable intake, process/helper replacement, delivery ordering |
| synthetic-qa-zns-fqa-import | 58421 / 58422 / 58423 / 58424 | Fresh reviewer B, serial import window | E synthetic apply/replay/reconciliation/removal rehearsal and imported runtime continuity |

`/root/stand_engineer` owns all preparation writes in
`.worktrees/functional-stands`, branch `codex/functional-stands`, base
`5d81be9a`. Lead allocates, checks readiness and authorizes freeze/release.
Reviewers own only their separate report/test evidence paths. Lead writes only
`docs/qa/fqa-lead-*`. Root owns integration and stage status updates.

Every stand requires separate PostgreSQL volume, fixture journal/provider state,
configuration, installation ID, role labels and test users. Only immutable
digest-addressed images built from the same reviewed integration commit may be
shared. No stand uses root-owned native PostgreSQL 55432. Preserve existing
stopped stands and unrelated resources.

## Current checks

- Elevated read-only listener inventory: no listeners on 58401–58424.
- Elevated read-only Docker inventory: only
  `synthetic-qa-zns-resume-postgres-1`, bound to 127.0.0.1:55432, was running.
- This is a point-in-time collision check; engineer must repeat before binding.
- Reviewer A created with no inherited conversation. Plan completed at
  `docs/qa/fqa-flows-20260930-plan.md`: F01–F13 remain untested.
- Reviewer B creation initially failed with `agent thread limit reached`. After
  two root Code QA assignments finished, a fresh no-history B was created. B is
  plan is at `docs/qa/fqa-recovery-import-plan.md`: R01–R18 and I01–I09
  remain untested. Two independent reviewer assignments now exist; no runtime
  PASS. This temporary pressure did not establish a sustained slot requirement.
- Product integration can advance independently of stand seam preparation. Root
  reported two relevant static Code QA passes; stand overlay focused gates and
  fresh review remain required before a frozen acceptance epoch.
- No stand is READY or FROZEN. No Functional QA acceptance has executed.

## Readiness prerequisites

1. Root supplies the final immutable merged candidate and affected independent
   Code QA results. Stand overlay and agent-fixture capability must be reviewed
   and included. Never relabel an unreviewed preparation build as an accepted
   baseline.
2. Engineer records commit, source manifest, image digests, compose project,
   bound ports, volume names, database roles, installation IDs and local-only
   adapter endpoints. Prove the three state stores do not share mutable state.
   For managed replacement, actual running `Config.Image` must contain a
   resolvable `repository@sha256` reference; inspect ImageID and local RepoDigests
   must correspond to the build provenance manifest. A sha256 image ID alone
   does not establish the managed replacement binding.
3. Usable Telegram-like UI supports visible manual and fixture-agent messages,
   inline buttons/reply keyboards, callback acknowledgements, prior-message edits,
   file uploads/downloads and relevant Web App entry points. Both EN/RU are
   accessible. A HTTP smoke alone does not establish this capability.
4. Reviewer access record contains public URLs, explicitly synthetic users and
   roles, events/capacity fixtures, controls and observation contracts. It must
   exclude source/diffs/history/prior findings. Secrets stay out of reports.
5. Recovery stand exposes isolated documented fault controls and wire timestamps:
   retry_after, repeated/ambiguous 429, recipient rejection, response lost after
   acceptance, credentials outage, database outage, lock-session loss, process
   signals/crash, managed replacement/helper termination and restart backoff.
   Persisted journals survive intended fixture restart or the limitation is
   explicit. Collector fault tests need an isolated collector endpoint.
6. Import stand has reproducible synthetic snapshots/plans/identity resolutions,
   manifests/expected counts and immutable receipt bytes, conflict/missing-file
   cases, a single-writer apply/reconcile window, archive/removal controls and a
   no-importer runtime build for post-removal restart.
7. Lead assigns the active writer and marks an explicit frozen epoch before
   reviewer execution. Routine preparation is ended. No rebuild/reseed during QA;
   reviewer controls only agreed isolated test faults. Release precedes upgrades.

## Acceptance matrix

| Requirement family | Reviewer / stand | Required proof | Current result |
| --- | --- | --- | --- |
| Manual-agent-manual and free text/media intent | A / flows | UI transitions and durable history, pending form does not trap unrelated input | Not executed |
| RU/EN and locale changes | A / flows | Both locales, prior-card refresh, declared fallbacks, stored UI locale | Not executed |
| Current ACL and privacy | A / flows | Ordinary user, event payment admin, global admin without event payment role, cross-event denial, role revocation, private memory/deletion | Not executed |
| Registration ordering | A flows + B recovery | Specific-event initiation, repeats, expiry preserving draft, reversed completion, restart and wire announcement order | Not executed |
| Knowledge/history freshness | A / flows | Authorized context, deletion/revocation, stale buttons/cards and uploads | Not executed |
| Durable intake/replay and bounded work | B / recovery | Crash/signal/drain across batch receipt, commit/delivery, offset and operation dedupe | Not executed |
| Physical managed replacement | B / recovery | Old app/helpers stopped before new writer, lock loss during active work, supervisor restart/backoff | Not executed |
| Throttling and partial outcomes | B / recovery | Durable deadlines, independent recipients/lanes, per-item state, opt-out, uncertain head handling, wire timestamps | Not executed |
| Safe operational evidence | B / recovery | Bounded correlations/counters, redaction, collector failure permits business work | Not executed |
| E imported state and removal | B / import | Apply/replay/conflicts/reconcile then no-importer restart preserving core identities/legacy refs/proofs/state | Not executed |
| Real model/identity/provider behavior | Separate final gate | Authorized real integrations, natural intent and language switching, live identity propagation | Not established by fixtures |

Known readiness limitation: pre-save consumed-turn and fixture-reboot scenarios
are not supported by the current capability handoff. They remain explicit
acceptance gaps until a documented black-box control permits execution; this
record does not omit or mark them passed.

## Freeze and release protocol

Engineer reports readiness with evidence, lead verifies and records the epoch,
then reviewer acknowledges exclusive execution ownership. Flows and recovery may
run in parallel only with separate state. Reviewer B runs recovery and import
serially. Import mutations and temporary receipt removal require an exclusive
window with runtime writers stopped. Engineer may perform a required preparation
step only after reviewer releases that stand. Each substantive product fix
requires a new reviewed epoch and fresh independent affected Functional QA.

Lead reconciles individual evidence into a combined report. PASS requires actual
execution for the declared scope; blocked/skipped/unsupported remain open. A
scoped result does not accept the entire architecture or migration.
