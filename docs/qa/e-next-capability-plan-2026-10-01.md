# E next capability plan — 1 October 2026

Status: read-only investigation complete. Implementation ownership is pending root approval. Integration base inspected: `e72338e1f1b83b259381a4dc3ea2e8afe42f3d8e`. No database, Docker, runtime, importer or product files were changed.

## Bounded next scope

Complete synthetic shared composition by binding `Options.Delivery` before `appservices.NewServices`. Current `platform/integration/notification_delivery_fixture_test.go` calls the constructor first, then rewires selected service values. Constructor-owned copies in `BotDelivery.Food`, `Registration.Intake.(*derivedmutation.NativeRegistrationResolver).Service.Registration` and `AdminUtilities.Registration` retain their original settings. The constructor already supports the required binding; a product constructor rewrite is unnecessary.

Reuse the unaccepted candidate `qa.local/go-resume-20260929/e-shared-composition-completion-20260930`. Its manifest owns exactly the fixture helper and a new copied-service test. Check its before/final hashes against the developer branch before reuse. The candidate test uses a nil pool and verifies that food reaches request validation rather than failing delivery settings, and that native intake receives valid settings. This checks an observable boundary defect rather than duplicating a constructor assignment.

Recommended branch: `codex/e-shared-composition-20261001`, separate worktree `.worktrees/e-shared-composition-20261001`. Exclusive file ownership:

- `platform/integration/notification_delivery_fixture_test.go`
- `platform/integration/shared_composition_copies_test.go`

Planning report ownership remains this file only until root grants the implementation paths. Root owns shared tracking documents. Stand lead owns new E materialization, images, databases, Docker, resources and FQA setup. No edits to `appservices`, migration SQL, importer or frozen historical evidence are needed for this slice.

## Local gates and handoff

Use branch-local `GOCACHE`, pinned Go tools and formatting. First reproduce the copied-service regression with the old helper and the focused test. Then run the fixed non-DB test without claiming PostgreSQL acceptance. Run affected PostgreSQL groups with a separately assigned local synthetic cluster and the existing per-test database helper: native registration intake/authority, food derived delivery, admin utility refresh, notification delivery runtime and shared queue groups. Run affected pinned lint/format on integration and appservices, plus compile checks. No shared database or Docker mutation is permitted until a concrete test-resource owner grants access.

Before requesting Code QA, update the developer branch against the current integration branch, resolve conflicts locally, rerun affected gates and commit only owned paths. Supply exact base/commit, clean worktree status, commands/results and candidate-to-commit mapping. Fresh Code QA receives requirements and immutable source/diff. Stand lead subsequently includes the reviewed slice in a new frozen E epoch; independent Telegram-like EN/RU Functional QA remains mandatory.

## Reusable full synthetic inputs

`qa.local/go-resume-20260929/e-final-schema-completion-20260930/final/source/manifest.json` declares 12 coverage domains and 23 records in 16 files: users, events, passes, orders, order_capacity, massage, messages, files, bot_storage, knowledge, schedule and configuration. Preserve the full source archive and `decisions.json`, `expected-draft.json`, `history-projections.json`, `source-record-projections.json`, `projections.json` and `probe-spec.json` beside it. These include shared pair-proof ownership, old history and long EN/RU bodies. Seven CLI import domains materialize the business data; knowledge/lineup are immutable permanent configuration resources rather than invented additional import receipts.

`e-materialized-20260930-02` contains genuine CLI verify/stage/plans, resolutions, permanent resource copies, `inputs.json` and importer digest `ad227f85fc2d6a16b254ac694744c63b12299e0a20ab4ea705912bce4d6d80f5`. Its status explicitly has `apply:false`, `reconciliation:false` and runtime acceptance pending. Its ledger expectation is 87 actual migrations through 088. This is historical preparation evidence, not the current 089 or pending 090 epoch. Preserve it unchanged.

Reusable removal harness: `e-importer-removal-20260930/final/rehearse.py`, its runtime probe, role inventories, Compose/config/replacement files and bounded no-CASCADE removal logic. Reusable cross-domain service probe: `e-runtime-coverage-20260930/runtime-coverage.go`, exact projections and credit projection SQL. These require refreshed bindings and actual execution; their presence is not acceptance.

## Final E dependencies

Final E materialization must wait for integration migration090 and its reviewed final source to settle. Stand lead creates a new evidence root and source inventory against that exact commit, derives the migration ledger from actual files/checksums, rebuilds the actual importer and product images, and runs real CLI verify/stage/plan/apply/replay/reconcile. Never transplant old plan digests, create substitute success receipts, or relabel the 088 binary/image as current.

After clean reconciliation, archive temporary receipts and isolated importer/module/executable, remove only assigned temporary resources using the preserved harness, and retain permanent legacy references, identity links, proofs, full history, drafts and resources. Build and run normal runtime without importer dependency or credentials, verify before/after permanent-state fingerprints, then restart and exercise real Telegram-like EN/RU interactions. Service probes supplement the UI gate. Independent Code QA and Functional QA must accept this final epoch; this two-file slice alone cannot close E.

## Next bounded capability — Git-bound E rehearsal harness

Shared-composition implementation is merged as `f2bcbc5c`; independent Code QA236 passed its exact two-file commit. The next request is plan-only. No script, stand or database changes are authorized by this plan alone.

Proposed separate branch: `codex/e-rehearsal-harness-20261001`, starting from the integration commit allocated by root. Proposed exclusive developer paths:

- `tools/e-rehearsal/prepare.py`: offline source/epoch binding, importer build and actual CLI preparation.
- `tools/e-rehearsal/rehearse.py`: bounded successor of the existing import/removal checker plus importer-absent build choreography.
- `tools/e-rehearsal/test_rehearsal.py`: focused filesystem, epoch and command-validation rejection tests.
- `tools/e-rehearsal/probes/removal/main.go` and `tools/e-rehearsal/probes/coverage/main.go`: reviewed copies of the two existing probes, changing only explicit synthetic database/marker selection required by the lead's new allocation.
- `docs/sandbox/e-import-removal/RUNBOOK.md`: exact interfaces, commands, stage boundaries and runtime probe/build handoff.

These paths keep the checker outside the removable `tools/migrate` package. Do not copy or alter historical evidence in place. Reuse the existing full synthetic source, decisions and projections through explicit paths and reviewed SHA256 inventories. Retain all existing probe assertions; use reviewed successor probe source paths only for the allocation delta. Probe source is copied into the isolated platform command tree for build, so its existing platform/internal imports stay valid. No runtime business implementation changes are proposed.

### Preparation contract

Inputs are an explicit full Git commit ID, expected last migration filename, explicit synthetic input-package inventory and a fresh evidence root. Check the selected commit and require current tracked runtime/importer bytes to match it; reject dirty/untracked source additions, symlinks/reparse escape, missing/extra source entries, mismatched supplied hashes and a pre-existing evidence directory. Enumerate the complete tracked `platform/` and `tools/migrate/` source from that exact Git epoch. Copy that source to the exclusively created evidence root, then derive the migration name/checksum ledger from its actual SQL files. Do not assume contiguous numbers:042 remains absent. The expected last filename must match, and ledger counts are derived rather than copied from088 expectations. Business count/projection expectations stay independently supplied.

Build the real importer from that marked source copy with GOWORK=off and branch/evidence-local cache. Record compiler, module list, source inventory, binary hash and build command/exit; no existing088 binary is accepted on filename alone. Invoke genuine verify/stage, seven plans and messages validation. Resolution files copy the reviewed explicit decisions and bind their newly generated plan hashes. Keep knowledge/lineup copies outside the runtime importer dependency. Emit inputs/provenance/preparation status only after corresponding real commands succeed, with apply/reconciliation/runtime acceptance still false. Do not synthesize successful CLI output in tests or production paths.

An explicit089 epoch can prove the offline harness during development, but its output must say089. Final E materialization waits for reviewed090 and the frozen integration commit/image inputs; the expected-epoch parameter never reclassifies088/089 evidence as090.

### Rehearsal contract and ownership

Reuse the original seven apply/replay phases, five supported explicit reconcile commands, immutable source/stage/input/resource bindings, complete permanent table/sequence fingerprints and exact imported owner/history/draft checks. Users/events expose reconciliation through their actual apply/replay summaries; the current CLI has no standalone users/events reconcile command. Do not invent additional commands or claim a narrower count-only reconciliation.

Consume a lead-approved stand allocation file rather than discovering or selecting a database automatically. It binds synthetic database/comment/owner, actually verified endpoint/execution topology, prerequisites project/Compose/env path, permanent config volume, local digest-addressed CLI image and runtime-source ownership marker. Preserve the original fail-closed endpoint, ownership, zero-managed-session and absence-of-existing-data checks. Reject wrong host/port/database/role/project/marker/resource binding before any action. The lead owns the allocation; the engineer owns infrastructure, Compose/images and ACK3 fake Telegram changes. This developer only invokes assigned rehearsal actions after allocation and root's execution permission. No FQA stand rebuild or lifecycle mutation is hidden in the checker.

Engineer coordination confirmed disjoint ownership. The current lead allocation at `docs/sandbox/fqa-stands/allocations.json` reserves import namespace `synthetic-qa-zns-fqa-import`, database `synthetic_qa_zns_fqa_import`, installation010400000203, configured PG/app/UI/control ports58421/58422/58423/58424. It is `preparation_not_frozen`; no import resources are started. Its current fields do not provide executable authority: final selected Git/schema, image bindings, exact volume/network names, source/input/config inventories, ownership marker, managed components/roles and writer/freeze status remain required. Configured host ports are not evidence of reachability: engineer observed unreachable PG/app host bindings on internal-only networks. The lead must choose and verify either the Linux CLI on its owned network or an explicit operator PG-front topology. Do not widen network access in the harness or silently fall back to another endpoint.

Both frozen Go probes explicitly reject any database other than `synthetic_qa_zns_e_import`; the coverage probe also requires the20260930 marker. Therefore they cannot run unchanged against the new allocation. The two proposed successor probe paths bind an explicit synthetic expected database/marker while retaining restricted role, missing importer schema, exact source projections, owner/proof isolation, full body/tombstone and all existing semantic assertions. This allocation change requires affected fresh Code QA. It is not justification for deleting the old guard or accepting arbitrary database names.

After genuine reconciliation, archive the exact seven temporary receipt tables, transactionally drop with RESTRICT and no CASCADE, then assert permanent fingerprints unchanged. Archive only the marked evidence-local importer source/module/executable and retain export, plans, resources and SQL history. Build normal product plus both hash-bound runtime probes from the importer-absent copy with a fresh cache and credential-free environment; record `go list -deps`/build outputs and hashes. Reject surviving importer files, references/dependencies or leaked migration DSN before declaring build independence. Start/restart and bilingual Telegram-like QA remain lead/engineer operations and cannot be inferred from a build or service probe. Do not print connection credentials or source bodies on command failures.

### Meaningful gates

Before external execution, run Python syntax and standard-library unit tests in temporary directories. Cover wrong Git epoch/migration tail, altered or missing/extra inventory, changed binary/input/resource hash, symlink/outside-path/source-root retirement rejection, existing evidence overwrite, wrong allocation and failed subprocess handling. Verify rejection occurs before invoking a destructive executor. These tests validate guards; they never stand in for real CLI/database/removal acceptance.

Run one actual offline build/verify/stage/seven-plan/validate pass against the explicitly selected reviewed development epoch and original synthetic package. No DB or Docker is needed. Preserve genuine CLI JSON and executable/source hashes in new branch-local evidence. Compile the successor probes in their isolated platform command paths and run pinned formatting/affected lint there; no database/runtime pass is inferred from those checks. No importer rewrite or dependency addition is planned. Fresh Code QA inspects the exact harness/probe commit, original requirements and frozen source/contracts. Root owns reviewer routing and tracking.

After090 review and lead allocation, fresh final materialization, actual apply/replay/reconcile, restricted runtime rebuild, archived importer/receipt proof, mounted resources, managed restart and independent EN/RU Telegram-like QA form the final E gate. Run these once per frozen epoch under the lead's exclusive mutation barriers. Historical088 evidence stays untouched.

## Final090 execution preparation — read-only update

The immutable first harness candidate `fde2b749` received Code QA242 changes
required. Its source and historical evidence remain unchanged. The successor
corrects effective pgx target validation, complete local-byte preflight before
database access, and food's explicit replay reuse contract. Root owns fresh
review routing; neither candidate is final E acceptance.

### Inputs and epoch boundary

The unchanged full synthetic package is
`qa.local/go-resume-20260929/e-final-schema-completion-20260930/final`.
Its `package-hashes.json` SHA256 is
`836b5ff09c31f2ba52be36d42400b0fb5706c5aa872b741a6aa3f35d47d377ca`;
source manifest SHA256 is
`ec0ec9b19305eb36d2219ab75dacd26e6cec39300cc4f17653f8a121b1ab4e44`.
Keep all twelve domains, 23 records and independently supplied decisions and
business projections. `expected-draft.json` SHA256 is
`a502c8faa16adae9ed31bc3d42176c7341d3a51d31bf42c07ecad2881f1208c1`.
The original coverage projection SHA256 is
`b631b367d9461e0e2a582e4f1d7a9bc29d7d11f7549a30661efba4710ac3b11e`.
Generated final projection successors may change only the reviewed allocated
database/marker and the actual history ID resolved from its fixed source key;
retain originals and record exact successor hashes. Never derive expectations
from observed business results.

After menu090 review and merge, select one immutable integration commit and its
actual last migration filename. Export complete tracked Git blobs into a new
owned evidence root; build CLI and runtime from those same exact source bytes.
Record the full SHA256 file inventory, raw SQL ledger inventory, actual compiler,
module graph, binary hashes and genuine command output. Do not use autocrlf
worktree bodies to predict the embedded migrator checksum. Derive migration
counts from real exported files (042 is absent), preserve predecessor ledger
checksums and applied_at values, and reject drift before apply. Old088 and
explicit089 exports remain historical evidence with their original epochs.

### Lead allocation and mutation barriers

Lead confirmation reserves database `synthetic_qa_zns_fqa_import`, namespace
`synthetic-qa-zns-fqa-import`, marker
`qa.e-import-removal.20261001.synthetic-only`, and nominal PG127.0.0.1:58421.
This is a reservation only. No endpoint, image, config, inputs, final schema or
freeze authority is established. The previous internal-only network's configured
host ports did not establish actual access.

Before execution the lead must bind final Git/migration, owner, reviewed_schema,
input_reviewed, status frozen, inputs/source/raw-migration/probe inventories,
probe manifest, exact Compose and env hashes, digest-addressed CLI/runtime
images, actual config/state/PG data volumes and network, and six managed
components. The execution allocation sets writer=e_rehearsal, managed_roles
zns_app/zns_meter, managed_stopped=true and a genuinely verified endpoint.
The current harness supports only a reviewed native host-loopback operator
topology. Its URL may have no query or only sslmode=disable; PG environment
indirection is removed. A Linux CLI on an owned network requires a separately
reviewed transport successor chosen by root/lead, not a new Daniel approval.
Engineer owns topology, images, config mounting, snapshots and lifecycle;
lead owns allocation, writer barriers and Functional QA stand freeze.

### Real import, removal and acceptance assertions

Run genuine verify/stage/seven plans and messages validation, then review their
new resolutions and hashes. Under the stopped-writer barrier run seven actual
apply/replays. Food replay must explicitly report reused:true; the other six
must explicitly report integer applied:0. All must reconcile. Run the five
supported standalone reconciles (orders/passes/food/massage/messages). No
invented users/events reconcile command or substitute success receipt is valid.

Assert imported owners101/202/303 retain identities/locales and zero identity
network calls; seven order refs, six pass refs, two food refs, four massage refs and
five message refs retain their independent projections. Preserve paid/unpaid/
cash order amounts and stale capacity reservations; pair shared-proof ACL,
two payment participants and the free pass; rejected historical meal18499RUB
versus current quote18500 and paid activity250000; finalized massage price57BYN/
1600RUB and one resumable owner-bound draft. Preserve sent notice states and
observe eligible notice progression without replay/restart duplicates.

Long EN history is14400 Unicode characters/19920 UTF8 bytes; RU positive control
is14030/21620. Preserve full-body hashes and older history. Synthetic deletion
must retain all five events/source refs, remove exactly the selected body,
create one tombstone and preserve the other long body. Preserve four proof
blobs/five proof refs, shared proof ownership, knowledge/lineup exact bytes,
three schedule slots and overnight Amsterdam date handling. Credit usage reads
may create one inherited owner202 account; no attempts, reconciliation,
adjustments or operator/policy changes are imported or invented.

Archive exactly seven temporary receipt tables. Drop only them and their schema
transactionally with RESTRICT, then compare every permanent table and sequence,
including SQL migration ledger. Archive only the marked evidence-local importer
module/executable, keeping original export, stages, plans, resolutions and all
receipt archives. Build normal runtime and both bound probes from retained
source with fresh caches; reject importer package, binary, dependency,
credentials and runtime mounts. Record importer-absent image provenance.

Independent EN/RU Telegram-like UI QA must use that exact importer-absent image,
exercise imported profile/history, order/pass/proof permissions, food historic
and current quote behavior, massage booking/draft continuation, long-body
read/delete/tombstone/control, knowledge/lineup and manual/mixed-agent flows.
Observe real callback ACK responses and permitted notification progression.
Engineer performs the actual managed replacement/restart; stopped-writer
capture/compare plus post-restart UI prove persistence. Service probes and
offline builds supplement this gate and do not replace it.

### Rollback resources and remaining dependencies

Before destructive phases, engineer retains an assigned synthetic PostgreSQL
snapshot, permanent volumes/config, exact source and image digests, raw ledger
with applied_at, and full pre-removal table/sequence fingerprints. Retain
post-removal fingerprints, seven receipt archives and retired importer
module/binary. Recovery restores only the assigned synthetic snapshot or
recreates a fresh assigned stand from the same reviewed Git/source package;
do not repair a partial import by deleting permanent rows or normalize an
already applied ledger. Preserve failed attempt evidence and writer ownership.

Final execution depends on reviewed menu090 integration, fresh acceptance of
the successor harness, and the lead's verified frozen allocation. Source/data
planning proceeds independently. No DB, Docker or stand writes are authorized
by this report, and the full E gate remains unaccepted until both independent
reviews and current runtime/UI evidence pass.

Written by E capability developer (gpt-6/Codex)
on behalf of Daniel Drizhuk
