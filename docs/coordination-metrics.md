# Coordination metrics

Baseline observation: 2026-09-30T22:04:53Z. Owner and sole CSV writer: kanban_manager.
Source: [coordination-metrics.csv](coordination-metrics.csv). Human view: [KANBAN.html](../KANBAN.html).
These metrics describe engineering handoffs, not product readiness or a completion percentage.

## Event record

Append an event after a verified state/owner/commit change. Fields: observed UTC,
task ID from the existing board, object/scope, state, current owner, exact observed
commit/base, evidence and limitations. An initial observation is a baseline,
not proof that the task entered this state at that time. Never invent historical
start times. Root supplies verified events; the manager appends them and refreshes
the board. No competing CSV writers. Preserve prior rows.

A task's current state is its last event for the same task/object. Commit/base
fields may be empty when they do not apply or have not been verified. Branch
movement alone is not a completed gate or a new reviewer approval.

## Queue definitions

- ready_for_review: developer merge preparation complete, clean worktree,
  exact commit/base and required local gate evidence supplied; review not started.
- reviewing: fresh independent reviewer assigned to the exact immutable commit.
- ready_for_merge: relevant review applies to prepared code, worktree clean and
  affected gates green; waiting for the single merge slot.
- local_gates: required developer checks remain incomplete; excluded from both
  ready queues even when an older static review passed.
- preparing / running_gate: preparation or a specific gate in progress; no PASS.
- merged / integrated_pass: merge and affected integration checks respectively;
  neither means Functional QA acceptance.
- stand_preparing / stand_ready / stand_frozen: separate readiness and frozen-epoch
  evidence; planned infrastructure is preparing, not ready.
- fqa_planned / fqa_executed: source-blind plan versus actual execution evidence.
  Executed is distinct from accepted; skipped or untested is not passed.

Limit finished branches awaiting review/merge to 2–3; this limits accumulated
handoffs, not the requirement for 2–3 capability developers. Count each branch
once, including reviewing in the broader finished-branch backlog. If the backlog
exceeds the limit, prioritize review/merge before more finished handoffs.

## Waiting time and returns

Review wait starts at the first observed ready_for_review event and ends at an
observed reviewing event. Merge wait starts at observed ready_for_merge and ends
at observed merged. Stand wait belongs to a consumer waiting for a named stand,
ending at its observed frozen usable epoch. Report only elapsed time since the
first observation, explicitly a lower bound when the real start is unknown.
Initial historical starts are unknown. No estimated durations or retrospective
reconstruction from intentions. UTC timestamps keep ordering independent of locale.

Count return iterations only for explicit post-baseline events returning a handed
off task to its developer for a defect, conflict or failed required gate. An
initial known failure, an ordinary local edit or a metadata-equivalent rebase is
not a newly observed return. The baseline count is zero; prior iterations are
unknown, not zero historically. Record reason and prior reviewed commit.

Stand READY/FROZEN counts are based on the lead's readiness records, execution
counts on independent reviewers' actual evidence. Plans and synthetic unit tests
do not count as executed FQA. Track requirements → test → FQA in the existing lead
matrix instead of creating another acceptance registry.

## Standard handoff and frozen execution

A developer submits exact commit/base, clean status, commands/outcomes for
formatting, pinned lint and affected tests, conflict resolution and limitations.
The developer owns preparation; root does not clean developer gates or resolve
its conflicts. Substantive resolution after review needs fresh affected review.

Use one immutable acceptance epoch for an FQA batch. Build each reviewed commit/
image digest once and share immutable images among the three isolated stands;
mutable DB/provider/config state stays separate. No rebuild during a frozen QA
window. Requirements, code review and Functional QA remain independent.

## Initial observation

Integration: 133104f6. Catalogs merged5d81be9a, selected integrated gates passed.
Privileged reads 7e8dacee are ready for fresh review (QA230 not yet confirmed
assigned at this baseline); agent fixture ecfbd188 is ready for merge with
metadata-equivalent source and QA229 on b2263f13. Knowledge4db4b7c3 still waits
for final full lint, despite QA228 on equivalent a88c23c9. Thus ready review=1,
ready merge=1, finished queued branches=2. Return iterations since observation=0;
historical start/return counts unknown.

Three stands preparing, READY=0, FROZEN=0, FQA execution=0. Two independent
reviewers have 40 planned UNTESTED scenarios. Stand engineer's lint/full tests
passed; Linux race process3541 remains running per verified root event, without
completed race or image-build proof. No readiness or stage acceptance inferred.

## Second observation: 2026-09-30T22:06:15Z

QA230 actually dispatched on immutable7e8dacee; ready review=0, reviewing=1.
Knowledge prepared5f3ca030 is clean with full host tests/pinned lint green and
two source blobs equivalent to QA228; ready merge=2 including ecfbd188. Root
still verifies equivalence/base addenda before serialized merges. Finished review/
merge backlog=3, at the approved accumulation limit. No merge recorded yet.
Observed C01 review wait ended after82 seconds; D01 merge and stand consumers
have at least82 seconds since first observation; F03 merge wait starts now.
Historical starts remain unknown. Returns since baseline=0; READY/FROZEN/FQA
execution all remain0. All elapsed durations are static observation snapshots.

## Merge observation: 2026-09-30T22:10:07Z

Git HEAD5a4d2f27 verified. Knowledge5f3ca030 mergedcb65243f after QA228
successor source/base addendum; fixtureecfbd188 merged1c485ddd after QA229
addendum; privileged7e8dacee merged5a4d2f27 after QA230 PASS. All three merge
committer times are2026-09-30T22:07:40Z, independently read from Git. The optional
event_utc column preserves this authoritative event time separately from observed_utc.
Never substitute committer time for an unknown historical ready-state start.

Ready review=0, reviewing=0, ready merge=0, finished backlog3→0. D01 observed
merge wait closed after167 seconds; F03 after85 seconds, both lower bounds.
Root integrated gates and full Functional QA still pending. Returns sincebaseline0.
Stand overlay5cffa904 requires developer rebase/affected gates before fresh QA;
Linux race is now terminalPASS. Three configurations remain templates, not running
stands/images. READY/FROZEN/executed remain0; two reviewers'40 scenarios UNTESTED.
Observed stand wait is314 seconds since baseline, with earlier start unknown.
Next capability lanes: C modern-order policy and D model usage; new branch/database
assignments not yet verified. Root must refill actual capability assignments.

## Integrated observation: 2026-09-30T22:14:42Z

Integration5a4d2f27 scoped native/config command terminalexit0: six package outcomes,
678 test/subtest PASS, zeroFAIL, fiveSKIP. Pinned affected lint includingbot exit0,
zeroissues. Record each of three merged task objects as integrated_pass, but these
are the same command and must never be summed as three runs or as product coverage.
TEST_DATABASE_URL intentionally empty. Skipped: CombinedClientRunsRegistrationBatchPostgres;
RuntimeServerLossCancelsAcceptedRequest/running and /draining; DeliverySurvivesRestart;
ZitadelLocalAdapter. Full PG/lifecycle/Zitadel/FQA acceptance remains unproven.

New capability work actually assigned: four-path order host policy on
codex/c-order-host-policy and ten-path model usage on codex/d-model-usage,
both from5a4d2f27. Exclusive zns_model_usage_qa DB was created after absent check.
No reviewed final commits/gates claimed for those new tasks. Ready queues remain0;
three stands preparing, READY/FROZEN/FQA executed0. Observed stand wait589 seconds
since baseline; historical start unknown. Returns since baseline0.

## Reconciliation observation: 2026-09-30T23:01:21Z

Manager restored after root committed00ad10e3; root relinquished the three tracking
files. Git HEAD00ad10e3, product source81a9cf73. Read current QA233/234/235 reports,
public handoff and A's actual execution report. Historical rows stay unchanged;
new records reconcile observed state without reconstructing when unobserved events
started or finished.

Model usage merged e72338e1 after QA233. Root usage gate135PASS/7SKIP/0FAIL includes
four package outcomes; no PG endpoint. Models/credits/broadcast catalog merged
81a9cf73 after QA235. Root host gates968PASS/84SKIP/0FAIL comprise966 testcase PASS
and two package outcomes, without PG. All six affected package pinned lint gates
passed with zero issues. These are separate scoped commands, not the full failing
baseline or a full lifecycle/data-preservation acceptance. Original Windows
EvalSymlinks failure evidence preserved.

Three capability lanes active: C registration context/current authorization nine
paths; D job progress nine paths; E shared composition two paths. F02 menu SQL fix
was transferred after QA234 finding, counted as one observed return since baseline.
No finished branch awaiting review/merge reported at this observation. Do not
interpret zero ready queue as no unfinished work.

Stand seam QA231 mergedce427ca5. Two manual-scope stands handed off with frozen
image digest; A completed one scoped UI report and released flows ownership to
lead, B remains active on recovery. Import unprepared. fqa_executed counts one
finished independent batch, not whole-stage PASS. Original40 plan rows remain:
F13 retain partial/blocked obligations, R18 active scoped execution, I9 not handed
off. A observed seven bounded PASS rows and one fixture capability success; no
full C or migration acceptance. Callback acknowledgement remains blocked.

Current stand states: flows released manual-ready, recovery frozen manual scope,
import preparing. Full-scope ready count is zero; two manual handoffs exist, one
active frozen stand, one completed scoped report. Historical zero stand/FQA rows
are superseded, not rewritten. Exact earlier freeze/release timestamps unavailable;
never report an invented duration for those completed waits. Ongoing import wait
has been observed56min28s since first baseline; earlier start unknown.

## Fix ownership observation: 2026-09-30T23:04:55Z

Root transferred090 preservation proof to menu_sql_developer as part of23 approved
paths: original21 plus queue and credit observation upgrade tests. Genuine087/088
→090 old-ledger/all-row/replay assertions remain mandatory. Developer resolves
scope/gates locally including full affected store PostgreSQL before fresh review;
root owns overall E-epoch integration. No extra migration logic added. This is a
scope refinement during the existing return iteration, not a second return.

## Targeted observation: 2026-09-30T23:06:53Z

B completed and released recovery. Read B's independent report and lead's combined
40-row accounting: A13 rows retain blocked obligations; B R01PARTIAL, R02–R18
BLOCKED, I01–I09BLOCKED. Two completed scoped independent reports, zero active
reviewer freeze, both stands preserved/released to lead; full-stage readiness0.
Do not interpret lower-level manual PASS as a full original scenario PASS.

Engineer acknowledged only the approved three-path next-control plan/branch;
no new build or epoch. E prepared382b778b/base00ad10e3 developerPG65PASS0SKIP/lint0
now on fresh QA236. Ready review0, reviewing1, ready merge0. C9 units green final
lint pending; D initial synthetic alias fixture failed/corrected next gate pending;
menu23 gates/090 proof in progress. No green handoff inferred for C/D/menu.
Returns remain1: a failed local preparation fixture does not count as a new
review→developer return. Import wait observed62minutes, historical start unknown.
Exact completed A/B freeze/release timestamps still unavailable; no invented waits.

## Post-commit observation: 2026-09-30T23:19:55Z

Root acknowledged9c5cbc41 including42 prior CSV events; exclusive tracking resumed.
QA236 PASS shared composition382b778b mergedf2bcbc5c. QA237 PASS registration
revalidation4f071bb1 merged1b729672; current root source/HEAD1b729672. Root host
native985PASS/84SKIP/0FAIL comprises983cases plus2packages, not PG. Previous usage
135PASS/7SKIP is a separate historical command; no whole baseline/FQA accepted.

Actual next C lane: six approved registration fixture paths on
codex/c-registration-fixture and exclusive synthetic_qa_zns_registration_fixture
DB. E lane: six offline CLI/removal harness paths on
codex/e-rehearsal-harness-20261001. Lead retains infrastructure/final090 execution
ownership. D nine paths final realPG gate active e297bbdc/base1b729672. Menu23
rebased76b70790 on1b729672 with owned-content equivalence; finalPG/race wait for D,
no green handoff. Engineer three code files sandbox gates active; no image/stand
writes. Ready review/reviewing/merge=0/0/0. Do not classify an active gate as PASS.

Three capability lanes remain active, fix separate; two previous scoped FQA
reports remain partial, all40 original obligations preserved. Stands preserved
and released, full C/D/E readiness/acceptance still NO-GO. Import ongoing observed
wait1h15m2s since baseline; historical start unknown. Returns since baseline1.

## Tiny transition observation: 2026-09-30T23:22:25Z

D e297bbdc/base1b729672 realPG terminalPASS reported135.951s; resource released.
Clean local gates complete and fresh QA238 actually reviewing, report pending.
Record gate duration as reported evidence only, not reconstructed queue wait.
Menu finalPG/race resumed after D release; no terminal PASS handoff. C6 fixture
and E6 harness actual development continue. D next source-observations scope is
planning only/unapproved, not another active development assignment.
Ready review/reviewing/merge=0/1/0; ongoing import observedwait1h17m32s, historical
start unknown. No other queue duration inferred. Files paused for root commit.
