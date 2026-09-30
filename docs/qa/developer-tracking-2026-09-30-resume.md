# Assignments and fresh Code QA routing — resumed 30 September

## Current routing and Git transition

Fresh requests through226, next227. All authorship/routes Codex under override;
Claude remains unavailable and unprobed. Historical snapshots below are superseded.

| Request | Scope | Result |
| --- | --- | --- |
| 212 | Long registration operation ledger | Static PASS; applied, focused PG PASS |
| 213 | Whole-job observations | Changes required; successor external |
| 214 | Registration/queue quality fixes | PASS; applied, affected tests/lint clean |
| 215 | Identity cache diagnostics | Changes required |
| 216 | Knowledge host catalog/preparation | PASS; applied, formatting addendum PASS |
| 217 | Provider rev3 and stand rev7 | Static PASS; unapplied, runtime pending |
| 218 | Menu retirement rev6 | Changes required; transferred to another developer |
| 219 | E final-schema preparation | Static PASS; actual CLI plans generated, no DB acceptance |
| 220 | Cache diagnostics successor/binding | PASS; applied, selected tests PASS; lint findings separate |
| 221 | Knowledge error handling rev2 | Changes required; rev3 external |
| 222 | Model usage diagnostics | Changes required; rev2 external |
| 223 | Registration host catalog | Static PASS; unapplied |
| 224 | Host/cache quality correction | Static PASS; integration gates pending |
| 225 | Menu retirement rev7 | FAIL; two authority defects and one SQL cancellation defect; branch correction assigned |
| 226 | Knowledge SQL/cancellation rev3 | P2 finding; mixed ConnectError causes; transferred to another developer |

Daniel authorized Git consolidation and developer worktrees. Root creates a
checkpoint of applied state and serializes integration merges. C developer owns
food host catalog; D developer prepares actual agent-flow stand capabilities.
Existing frozen candidates stay immutable during migration to task branches.
Checkpoint ce994271 is on feature/go-platform-sandbox. Active branches:
codex/c-host-catalogs (C developer), codex/d-agent-stand (D developer),
codex/menu-authority (batch_fixture_fix), codex/host-cache-quality (menu_retirement_fix).
Each has a separate worktree and explicit local gate responsibility. Root serializes
merges; no push. Preserved caches are excluded from Git, not deleted.
Dedicated kanban_manager now owns KANBAN.html and optional docs/kanban-process.md;
root commits the finished board. Menu branch owns isolated synthetic PostgreSQL
database zns_menu_authority_qa, not the shared zns database or container lifecycle.
Knowledge error correction transferred to provider_compatibility_developer on
codex/knowledge-error-precedence at ce994271 after repeated failed static reviews.
No extra slots required at this point. Same-reviewer applied-source addenda do not
increment the fresh request counter.

## Historical coordination snapshots

All assignments use Codex under Daniel's temporary override. No Claude probing or launch. This coordination record is excluded from reviewer input.

Continuous capability developers: c_registration_developer (C shared target binding), d_observability_developer (D queue observations), r108_resume (E importer removal rehearsal). Root integrates actual source and exclusively executes shared gates/stands. Fix developers own separate external candidates: r109_resume (menu retirement), r56_cancelled_receipt (receipt tests/lint), r77_payment_redaction (deleted-order message), r110_resume (provider and stand binding).

| Fresh request | Authorship | Route | Scope / result |
| --- | --- | --- | --- |
| 189 | Codex | Codex, override | Own cancelled-registration receipt candidate; static clean |
| 190 | Codex | Codex, override | Payment retirement first candidate; changes requested |
| 191 | Codex | Codex, override | Menu retirement first follow-up; changes requested |
| 192 | Codex | Codex, override | Deterministic receipt timestamp fixture; static clean, not applied separately |
| 193 | Codex | Codex, override | Provider rev6 + managed stand rev4; changes required |
| 194 | Codex | Codex, override | Payment retirement rev2; changes required |
| 195 | Codex | Codex, override | Shared registration target builders; static PASS, applied |
| 196 | Codex | Codex, override | Menu retirement rev2; changes required |
| 197 | Codex | Codex, override | Receipt observation rev3; static PASS, applied |
| 198 | Codex | Codex, override | Queue observations + runtime binding; upgrade coverage finding |
| 199 | Codex | Codex, override | Root target fixture parallelism/formatting; static CLEAN |
| 200 | Codex | Codex, override | Stacked shared registration menu reads; static PASS, applied |
| 201 | Codex | Codex, override | New developer menu retirement rev3; changes required |
| 202 | Codex | Codex, override | Root menu fixture parallelism/formatting; static PASS, applied; affected lint clean; interaction101PASS |
| 203 | Codex | Codex, override | Queue observations plus populated087 upgrade proof; ledger proof gap |
| 204 | Codex | Codex, override | Actual Go-fake provider seam plus standrev5; changes required |

| 205 | Codex | Codex, override | Queue upgrade proof rev2; old applied_at proof gap |
| 206 | Codex | Codex, override | Registration home reads; cancellation finding |
| 207 | Codex | Codex, override | Registration batch admission; static PASS, unapplied |
| 208 | Codex | Codex, override | Menu rev4; two P1 findings; epoch invalidated by author mutation, no certification |
| 209 | Codex | Codex, override | Queue observations, binding and complete upgrade proof rev3; static PASS, native gates pending |
| 210 | Codex | Codex, override | Registration home reads rev2; running |
| 211 | Codex | Codex, override | Actual Go-fake rev2 and managed standrev6; running |

Next fresh request: 212. Same-reviewer formatting-equivalence addenda 179, 186 and 197 do not increment the counter. Static acceptance does not accept runtime or Functional QA. Eleven concurrent slots currently suffice; capability assignments must be refilled as they finish. Capability developers now continue durable batch admission, source-refresh observations and complete synthetic E inputs. Historical/contact, whole-job summary and background correlation candidates remain frozen pending gates. Repeatedly failing provider, payment and menu fixes transferred to new Codex developers; root serializes shared delivery-worker integration and requires refreshed affected reviews.


Current staffing refresh: three active capability developers: C host capability completion (c_registration_developer), D cache/model observations (d_observability_developer), E shared composition completion (r108_resume). They first identify actual remaining required implementation and do not fabricate features when only acceptance remains. Menu fixes run separately in immutable rev6; payment integration waits on the shared worker base. Three fresh independent reviews 209–211 run concurrently. Root owns actual source integration, stands and shared gates. Eleven slots currently suffice.
