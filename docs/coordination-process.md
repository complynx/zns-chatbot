# Execution coordination

Daniel authorized these improvements on 1 October 2026. They supplement the
existing Git and independent QA workflow; no quality or acceptance gate is removed.

## Heavy checks

Root grants one native Windows heavy-check slot and one Docker/WSL heavy-check
slot. Full lint, whole-module compilation and race/integration suites request a
slot with owner, exact frozen source, command, environment, resource bounds and
evidence path. Quick focused checks on independent resources can continue.
Linux-target lint running as a Windows executable consumes the Windows slot.
No build or test may mutate an occupied Functional QA stand.

Existing checks are allowed to finish. Do not terminate or restart them because
their output is quiet or an observation times out. Observe the same tool handle;
if handles are scoped to another agent, that owner reports the actual process and
terminal outcome. A transient observation failure is not a process failure.
Root releases a slot after terminal evidence or an explicit owner interruption,
preserving interrupted evidence as interrupted. New checks wait for the slot.

Priority: the accepted registration-clock dependency and its operator/background
result closure, followed by final-byte checks needed for a ready review. Other
independent product development continues. Never edit source during a gate and
present that gate as proof of the new bytes; freeze a successor and refresh the
affected checks. Do not relax linter configuration or scenario budgets.
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

## Documentation and acceptance closure

Update working PROGRESS and the board after material results, ownership changes,
plan changes and blockers. Root batches the matching progress, board, metric and
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
