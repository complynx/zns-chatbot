# Execution coordination

Daniel authorized these improvements on 1 October 2026. They supplement the
existing Git and independent QA workflow; no quality or acceptance gate is removed.

## Heavy checks

Daniel authorized Linux-only checks on 1 October 2026. New builds, linters,
SQL generation, unit/race/integration suites and automated browser checks run
in Linux Docker or WSL. Native Windows checks are not required and must not be
started. A Windows executable targeting GOOS=linux is still a Windows check.
Root grants three Linux heavy-check slots across all stands. Full lint,
whole-module compilation and race/integration suites request a
slot with owner, exact frozen source, command, environment, resource bounds and
evidence path. Quick focused checks on independent resources can continue.
A granted owner slot includes preparation for that same approved check:
owned formatter scratch work and compile preflight on the same bounded resources.
Report changed input hashes, actual handles and terminal evidence without another
grant pause for ordinary preparation. New scope, shared-resource writers or product
changes still need root coordination; preparation does not accept a gate.
Use pinned tools and task-owned writable caches; shared module caches and frozen
source are read-only. Do not run pinned-tool builds outside the slot queue.
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
Start with at most two CPUs and four GiB per heavy container; request an explicit
resource adjustment if necessary instead of hiding an OOM or changing gate budgets.
No build or test may mutate an occupied Functional QA stand.

The Windows transition is complete; no Windows checks remain queued. Historical
receipts remain valid only for their recorded scope.

Use separate owned development and Functional QA stands, with independent data,
networks and evidence paths. Heavy-check concurrency is limited across stands,
not by requiring a single mutable stand. The final acceptance stand runs the
frozen integrated composition and remains unchanged while QA owns it.

Existing checks are allowed to finish. Do not terminate or restart them because
their output is quiet or an observation times out. Observe the same tool handle;
if handles are scoped to another agent, that owner reports the actual process and
terminal outcome. A transient observation failure is not a process failure.
Root releases a slot after terminal evidence or an explicit owner interruption,
preserving interrupted evidence as interrupted. New checks wait for the slot.

Priority: the accepted registration-clock dependency and its operator/background
result closure and bounded Telegram resends, followed by final-byte checks
needed for a ready review. Other
independent product development continues. Never edit source during a gate and
present that gate as proof of the new bytes; freeze a successor and refresh the
affected checks. Do not relax linter configuration or scenario budgets.
Measure request-to-grant waiting and actual gate duration separately. Record
container CPU/memory and OOM/exit state when a slot completes. Start with three
slots; retain the extra capacity only while it reduces queue time without
increasing gate duration or starving Functional QA. Reduce concurrent checks if
resource contention is observed, never weaken a gate to fit concurrency.
Current owners/handles and requests are recorded in the existing coordination
metrics; no new scheduler or helper service is needed.

## Functional QA access and fault windows

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
