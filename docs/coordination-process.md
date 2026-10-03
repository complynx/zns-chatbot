# Execution coordination

Daniel authorized these improvements on 1 October 2026. They supplement the
existing Git and independent QA workflow; no quality or acceptance gate is removed.

## Outcome-first AI workflow

Daniel authorized these optimizations on 3 October 2026.

- Keep one stand-support lane for C–E allocation, lifecycle, diagnostics and
  operator preparation. Assign two product developers to unfinished approved
  capabilities or verified boundary defects, with disjoint branch/file ownership.
  QA owns scenarios and verdicts, not product fixes. Do not count an approval-waiting
  worker as active or invent helper work to fill a developer lane. Preserve the
  C–E then remaining-parity sequence; no unrelated features enter the cohort.
- Assign a complete outcome: requirements, exact base/source, owned resources,
  permitted operations, resource/time bounds, evidence, next consumer and stop
  conditions. The stand owner carries preparation through actual readiness and
  release. Root coordinates shared custody and independent gates, not each
  formatter, receipt or ordinary owned preparation step.
- Before review, walk the complete consumer chain and reconcile discovery,
  emitted proof, host acceptance, phase predicates, source binding and deadlines.
  Batch related fixes into one immutable candidate with affected automated gates.
  Fresh independent Code QA still reviews every substantive final change; both
  Code and Functional QA remain required per stage. Do not create a separate
  review stage for each mechanical receipt or metadata-only identity update.
- For new stand contracts, separate declared safety/behavior invariants from
  native observations. Guard exact source/image, private-data boundaries,
  commands, environment contents, mount identities/access, resources, ownership,
  deadlines and meaningful lifecycle outcomes. Retain complete native capsules
  for audit. Incidental representation/order and transient Docker fields are not
  new behavior requirements by themselves. Declare typed identity and phase
  semantics explicitly; unknown security fields, extra writers, data changes or
  incomplete outcomes still fail closed. Revise an existing strict contract only
  in an explicit successor with focused preservation checks and fresh independent
  review. Never edit a frozen gate or normalize a failed result to obtain PASS.
- Use one short current handoff: original requirements, exact immutable candidate,
  ownership, terminal gates/limitations and next operation. Keep history outside
  it. Functional reviewers receive only the current source-blind requirement
  packet and a separately frozen actual-access annex, never implementation hints,
  prior findings or author conclusions. Read current owned paths before searching
  archived qa.local trees and caches.
- Use the existing local CSV to record request, preparation, ready-for-review,
  review start/result, integration, stand freeze, Functional start/result and
  custody release as observed events. Separate preparation, execution, review,
  root queue and external-permission waiting. Measure whole-outcome lead time and
  independently accepted requirements; counts of tests, commits and assignments
  are not readiness or throughput. Unknown timestamps remain unknown.
- Prepare exact synthetic operator/access permissions and commands before short
  fault windows. Record scope, owner, identifiers, allowed data/lifecycle actions,
  original bounds and actual access result. Root grants and general synthetic
  permission do not override sandbox admission. An approval-layer denial remains
  NOT RUN; obtain the required direct approval without an alternate route or quiet
  retry. Advance independent work while that action waits. Production, push and
  publication remain separately authorized.

## Heavy checks

Daniel authorized Linux-only checks on 1 October 2026. New builds, linters,
SQL generation, unit/race/integration suites and automated browser checks run
in Linux Docker or WSL. Native Windows checks are not required and must not be
started. A Windows executable targeting GOOS=linux is still a Windows check.
Root grants three Linux heavy-check slots across all stands. Full lint,
whole-module compilation and race/integration suites request a
slot with owner, exact frozen source, command, environment, resource bounds and
evidence path. Quick focused checks on independent resources can continue.
Count every running prerequisite, including idle PostgreSQL, runtime, observer
and browser containers, in the assigned slot's complete resource envelope.
Checker exit does not release a still-running prerequisite. Verify terminal
Docker state and custody before reassigning its capacity. Retain data volumes
when parking completed synthetic stands; do not infer historical aggregate
compliance from individual worker profiles.
Qualify the complete required toolchain and mounts before a heavy check. A
qualified image name does not prove that Node, Git, Go and the dependency closure
are all available in that container. Verify actual paths, pinned hashes and
versions together. Reuse qualified binaries read-only; do not rebuild unchanged
product images to correct a tool-only profile.
Qualify the exact supervisor command syntax in that image, not only its presence.
BusyBox and GNU timeout options differ. Keep the rejected command and its terminal
evidence; use a new owned receipt child for a corrected check, with unchanged
source and budget. Do not reuse a writable output still mounted by an old helper.
For CLI fixtures, prove that temporary executables can run from the actual
temporary mount and that PATH selects those fixtures before starting the suite.
Isolate GIT_DIR/GIT_WORK_TREE only for synthetic Git fixtures; retain the real
repository bindings for source guards and changed-test discovery.
A granted owner slot includes preparation for that same approved check:
owned formatter scratch work and compile preflight on the same bounded resources.
Report changed input hashes, actual handles and terminal evidence without another
grant pause for ordinary preparation. New scope, shared-resource writers or product
changes still need root coordination; preparation does not accept a gate.
Use pinned tools and task-owned writable caches; shared module caches and frozen
source are read-only. Do not run pinned-tool builds outside the slot queue.
Mount only a receipts output directory as writable. A read-only source mount
does not protect the same files exposed through a writable parent mount. Keep
frozen sources outside that output tree and record every source alias.
Bind execution hashes to the frozen reviewed manifest, not a new inventory of
whichever files happen to occupy the input paths. Check physical source/output
containment and the actual tmpfs options as part of CREATE-before-START guards.
Profile fixtures must preserve the observed native Docker representation. Check
positive cases against frozen sanitized native capsules, not profiles reconstructed
from the guard's own assumptions. Cover volume subpaths, network_mode=none and
running versus stopped network membership. Normalize only explicitly approved,
typed defaults; preserve private-value hashes instead of exposing credentials.
Compare unordered native collections by their complete typed identity: unique
mount destinations with every field retained, or unique environment keys with
every value and the exact entry count retained. Reject duplicate, missing, extra
or malformed entries. Preserve raw order in evidence. Keep order exact for
ordered contracts such as command arguments and declared HostConfig mounts;
do not assume environment order follows CREATE arguments.
For lifecycle admission, qualify Created, Running and Terminal separately before
a long observation window. Retain full native profiles and typed field-presence
differences. Do not compare a live profile with Created-only defaults or discard
changed fields. Bind exact phase expectations to the qualified image and native
evidence; a substantive guard change needs focused tests and fresh Code QA.
Before freezing a test change, inspect shared fixture setup and both success and
failure result contracts. Keep the discovered suite count, emitted proof and host
receipt predicate consistent. A passing suite with a failed host gate remains
scoped evidence; preserve the failure and qualify a distinct corrected attempt.
Retain each observed command exit immediately, before output-reader completion
or cleanup. Keep bounded partial output marked incomplete; do not parse it as a
complete container exit, identity or profile. Reader, timeout and cleanup failures
must retain the known command result and first failure as separate evidence.
Test these boundaries through the same adapter used for actual orchestration.
An elapsed-time assertion after a command returns does not enforce a deadline.
Use an owned Linux supervisor or qualified timeout with bounded child cleanup
inside the original total budget; retain nonzero terminal evidence on timeout.
Write Linux checksum manifests and owned Bash scripts as UTF-8 without a BOM
and with LF newlines. Preserve the raw source bytes; do not normalize product
inputs to repair a manifest.
Daniel's 2026-10-01 preference: reuse available caches when this is simple.
If cache access or setup delays a check, use a clean Linux build and standard
Go dependency download/verification from the committed go.mod/go.sum instead
of creating another cache-management task. Keep unrelated home-cache contents
outside containers. Go's test timeout applies to test execution; give cold
compilation a separate explicit build budget. Keep pinned tools and test gates.
For new Docker runs, keep Go build/lint caches and temporary compiler files in
task-owned Linux volumes rather than Windows bind mounts. Reuse a completed
warm cache only after its owner confirms no active writer; copy it once if the
new task needs separate custody. Record cache volume ownership and mounts.
Keep receipt exports separate from caches. This changes storage, not tools,
source, resource limits or test budgets. Do not move caches during a live check.
When Windows source I/O blocks an unchanged Linux gate, an owned native Linux
source copy is allowed. Verify every raw source byte original-to-copy before
and after the complete gate, plus exact HEAD/base/status/binary diff and all
declared user deltas. Keep source and Git inputs read-only. Do not normalize
line endings, call the copy a new canonical source, or combine partial failed
gates into a PASS. Preserve commands, pins, acceptance scope and budgets.
Start with at most two CPUs and four GiB per heavy container; request an explicit
resource adjustment if necessary instead of hiding an OOM or changing gate budgets.
No build or test may mutate an occupied Functional QA stand.

The Windows transition is complete; no Windows checks remain queued. Historical
receipts remain valid only for their recorded scope.

Use separate owned development and Functional QA stands, with independent data,
networks and evidence paths. Heavy-check concurrency is limited across stands,
not by requiring a single mutable stand. The final acceptance stand runs the
frozen integrated composition and remains unchanged while QA owns it.

Database-enabled credit observation upgrade checks require a dedicated
PostgreSQL cluster through TEST_CREDIT_UPGRADE_DATABASE_URL, alongside the
ordinary TEST_DATABASE_URL. A separate database in the same cluster does not
isolate the transaction horizon: concurrent old transactions can temporarily
exclude a newly created index from planning. The stand owner verifies distinct
cluster system identifiers before the gate. Keep the original index assertion,
test parallelism and budgets; do not force planner choices or skip the proof.
This routing applies to development, CI and final composition checks.

Existing checks are allowed to finish. Do not terminate or restart them because
their output is quiet or an observation times out. Observe the same tool handle;
if handles are scoped to another agent, that owner reports the actual process and
terminal outcome. A transient observation failure is not a process failure.

Before a recovery observation START, compare every frozen/current profile pair
and check the monitor's startup predicates against the retained predecessor
state. Keep raw representation differences and any declared typed-default
equivalence explicit; never normalize unrelated fields. A historical failure
exception must bind one exact attempt and end on a fresh START or a finite
operational deadline. Check actual event/container timestamps, not only arrival
order. Preserve old failures and all new failure predicates. This is not a
restart permission or an extension of readiness and health budgets.
Root releases a slot after terminal evidence or an explicit owner interruption,
preserving interrupted evidence as interrupted. New checks wait for the slot.

Priority: the next unmet dependency on the finite C–E acceptance path, followed
by final-byte checks needed for a ready review. Advance independent C and E
operations in parallel. Helper tooling must not delay an executable acceptance
or import operation. Other independent product development continues within
the approved sequence. Never edit source during a gate and
present that gate as proof of the new bytes; freeze a successor and refresh the
affected checks. Do not relax linter configuration or scenario budgets.
Measure request-to-grant waiting and actual gate duration separately. Record
container CPU/memory and OOM/exit state when a slot completes. Start with three
slots; retain the extra capacity only while it reduces queue time without
increasing gate duration or starving Functional QA. Reduce concurrent checks if
resource contention is observed, never weaken a gate to fit concurrency.
Current owners/handles and requests are recorded in the existing coordination
metrics; no new scheduler or helper service is needed.
Resume a completed worker with an explicit follow-up task; an informational
message alone does not start work. Confirm acknowledgement and current running
status before counting that worker as active or waiting for its next result.

## AI-owned execution windows

Daniel authorized these process updates on 2 October 2026. Assign a complete,
bounded outcome with exact source/base, owned paths and synthetic resources,
permitted operations, command/environment, resource bounds, evidence paths,
success criteria and stop conditions. The owner handles ordinary preparation
within that window without requesting each formatter, fixture permission or
known configuration action separately. Stop for changed source/scope, shared
writers, unexpected data or a failed prerequisite. Product fixes still follow
separate branches and fresh review. Production and security changes retain
their authorization boundaries.

Before constructing or handing off a runtime, use the existing tools to prove
actual volume UID/modes, command/environment compatibility, roles/DSN, image
binding and observability. Readiness requires all required processes Running,
required health checks and runtime admission; created containers, compilation
or a journal alone are insufficient. After readiness, observe stability until
freeze. Do not rebuild a stand while Functional QA owns it.

For a read-only Linux check profile, verify the actual module-cache layout,
tool paths and entrypoint before compilation. Bind both GOTMPDIR and TMPDIR
to owned Linux scratch space; CGO can use TMPDIR independently. Verify required
child mountpoint directories exist before freezing a read-only parent volume.
Compare every required control mount with the last qualified recipe, including
Docker inventory access when the existing guard requires it. A read-only socket
mount does not restrict Docker API operations: the granted script may use only
the explicitly authorized operations. Keep failed setup evidence distinct from
tests or SQL that never ran; repair the profile, not the product or gate budget.

Bind data guards to the original accepted readback or snapshot producer and its
hash. Preserve typed identifiers, source-key transformations, full-body joins,
serialization and ordering; actor labels and short projections are not substitutes.
If a compound guard fails, run one bounded read-only comparison of its predicates
or object fingerprints before requesting another mutation. Report object names,
counts and digests, not private row bodies. A failed guard proves no data drift
until the original representation and comparison algorithm have been checked.
An application read or successful probe is not necessarily transactionally
read-only. Identify committed lazy initialization or audit effects from source
and capture an immediate post-probe snapshot. Keep the original baseline intact;
justify a successor baseline with exact authorized effects and unchanged objects,
not arbitrary current values. Use native read-only transactions for diagnostics.

Prepare bounded diagnostic capture before reproducing a runtime failure. Retain
events, timestamped health/exit snapshots and relevant read-only session state;
capture logs and resource/OOM state at the first failure before retirement
removes the objects. Preserve the original scenario and readiness budgets.
Keep capture active through the complete runtime and QA window, not only the
initial stability probe. A past healthy window does not prove current readiness;
retirement invalidates READY, freeze and dependent reviewer dispatch.
Missing failure evidence calls for an observed reproduction, not blind retries,
an assumed product defect or a longer timeout. Reuse existing capture tools;
do not create a general framework for one blocked scenario.

Keep handoffs small: exact commit/base and ownership, terminal gates and evidence
links, acceptance boundaries and next operation. Root consumes ready independent
work promptly rather than serializing it behind unrelated preparation. Reuse
immutable images and released warm Linux caches when input equality is proved.
Focused developer feedback does not replace full acceptance or final CI gates.

## Test classification ownership

QA owns the slow/medium-long inventory and conservative affected-domain mapping.
Developers own runner mechanics and provide scenario intent. Classification uses
actual eligibility/deadline waits and dependency families, not names or one noisy
duration receipt. Short bounded timer assertions are not automatically slow.
Unknown/shared changes select conservatively; they must not omit affected slow
scenarios. Review the whole family when a shared helper or domain changes.

The classifier authors the manifest, focused classification assertions and
documentation on an owned branch. This audit is not independent acceptance:
different fresh Code and Functional reviewers inspect the final immutable
candidate within their respective scope. Existing full/default/final gates,
tests, budgets and required acceptance remain unchanged. Run the slow tier for
affected areas, critical acceptance, final refactoring and final PR CI; do not
claim execution speed gains from discovery alone.

## Functional QA access and fault windows

Daniel explicitly authorized changing synthetic test stands and advancing their
test time on 2 October 2026. Production is excluded. Coordinate the sole writer
and release active scenarios before a conflicting change. Record the requested
clock target, current revision, actual transition and readback; preserve scenario
budgets and monotonic time constraints. Routine authorized synthetic operations
do not require a new human decision for each window.

The public packet names allowed actions and actual capabilities. Prohibitions
must be specific: no dependency/installer or unrelated/personal downloads does
not prohibit required downloads of owned synthetic fixtures into QA evidence.
Missing adapters and observations are BLOCKED, never passed by inference.

Before arming a short-lived failure, reviewer and lead agree the exact resource,
sole writer, input identity, available controls, immutable source/image, timing
bound, operator command, evidence paths and return of ownership. Finish credential
injection setup, permissions and tool approvals before arming. Start the read-only
browser observer before the actual operation; notify it at the actual arm/install
boundary rather than an earlier preparation GO. Do not lengthen the original
failure budget to hide coordination delay. A missed interval remains a limitation.

The reviewer releases only the necessary resource to the operator and resumes
after explicit operator RELEASE. Preserve old journals and durable metadata;
no reset/reinsertion is allowed to manufacture the scenario. Lead owns lifecycle,
the reviewer owns observable UI results and the independent verdict.

## Final acceptance cohort and preflight

Root maintains one finite C–E cohort in the existing local Kanban: accepted
registration clock, callback ACK, identity revocation cache, diagnostics, clock
operator plus setup/role consumers, background results/schema091, and delivery
retries/schemas092–094, shared queue completion and late confirmed receipts.
Record exact reviewed commits and the final
integration commit. Unrelated work stays on separate branches until this cohort
has a frozen stand and actual acceptance results. Necessary defects remain in
scope; a substantive correction requires affected fresh review and a new freeze.

The FQA lead prepares isolated stands against that exact composition. Before
handoff, prove migrations and required roles, real bot/actor identity, immutable
fixture/setup anchor, operator permissions, EN/RU Telegram-like UI, provider
fault/restart controls, source/image hashes, evidence paths and sole writers.
Prepare credentials and observers before arming short fault windows. Readiness
must be demonstrated by preflight, not inferred from successful compilation.

After final merge and integrated checks, freeze the stands and dispatch the
original Functional matrix and actual E apply/replay/reconcile/removal/restart/UI
blocks. Root records last required merge, freeze, QA start and terminal result;
lead records executed/pass/fail/blocked/skipped original scenarios. Keep E
execution ownership explicit; helper preparation does not close E. Use the
existing local board and CSV, with no new scheduler or metrics service.

## One-off import recovery

Daniel approved whole-target database rollback or recreation followed by a complete
reapply for the one-off migration. Do not build per-record crash recovery, partial
resume or an exhaustive importer interruption matrix. Keep atomic domain writes,
explicit identity mapping, owned history/proof bytes, domain order, reconcile and
exclusive-writer checks. The importer must not automatically erase a database.
If synthetic allocation fails before database creation, preserve the failure and
inspect the exact owned partial resources. Empty owned resources may be removed
and a separately recorded corrected allocation may run within the same budgets.
Do not build a generic partial-allocation resume framework for this rehearsal.

QA validates the changed source independently and performs one isolated whole-DB
reset/reapply rehearsal with the same input and important-state comparison.
Superseded partial-recovery scenarios are not passed scenarios. This scope change
does not weaken runtime reliability gates or authorize production changes.

## Documentation and acceptance closure

Update ignored management.local/PROGRESS.html and management.local/KANBAN.html after material results, ownership changes,
plan changes and blockers. Working HTML stays outside Git. Operational journals and current estimates also stay local. Root batches stable rules and
report changes into one coherent local checkpoint per handoff/result group.
Do not create separate commits for every minor status sentence. Allocate review
numbers before dispatch as already required. Preserve user edits separately.

Root prioritizes the finite clock → operator → background results/schema → final
composition → frozen stands → original C–E acceptance/import execution chain.
Two–three product developers and parallel verified-defect fixes remain active.
New helper work needs a named blocked scenario and an immediate consumer.
Partial interface checks and prepared plans do not close a whole requirement.

Kanban manager tracks heavy-check requests/grants/terminal outcomes, repeated
returns, last required merge → freeze → actual QA, and closed/added/remaining
work. Use observed timestamps only. Root owns slot/integration decisions; lead
owns stand/fault windows. Estimates decrease only for proved acceptance closure.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
