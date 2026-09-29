# Independent Opus readiness assessment — 2026-09-29

This is a fresh readiness assessment, not a numbered Code QA request.
Launcher evidence: Claude CLI exit 0; JSON subtype success; is_error false; duration 220420 ms; 61 turns. `modelUsage.claude-opus-5-5.canonicalModel` confirms `claude-opus-5-5`, provider `firstParty`.
Prompt and raw response are retained locally under `qa.local/go-resume-20260929/opus-readiness/`.

Interpretation limits: the assessor had Read only, sampled about 30 files, and ran no checks. It followed requirement-document references to `DEFERRED.md` and `docs/architecture-stage-c-plan.md`; some assertions therefore depend on historical developer records rather than current independent execution. It explicitly ignored an embedded earlier budget/percentage. Its claim of a clean Git tree is not current verification: the prompt disclosed pending unvalidated working-tree repairs. Source concerns marked uncertain require reproduction. Engineering mechanism choices are recommendations for implementation, not automatically decisions the user must make. Do not weaken required gates based on the report's suggested disposition of failures or deferrals.

The independent assessor's response follows verbatim.

---
# Readiness assessment: Python → Go/PostgreSQL migration (zns-chatbot)

**Verdict: not ready for production (NO-GO).** Much of the required behavior appears in source. Almost none of the final release gates has passed. I estimate the remaining engineering effort at 45–90 person-days (8-hour days), most likely 55–70.

## Method and limits

- **Documents read:** the four allowed requirement docs, `docs/architecture-stage-c-plan.md` and `DEFERRED.md`. The allowed plan names DEFERRED.md as the governing deferral record. I treated its status notes as developer claims, not acceptance.
- **Estimates ignored:** `architecture-refactor-plan.md:267` contains budget and percentage figures. I did not use them. Everything below is my own assessment.
- **Tools:** only Read was available in this session; Grep and Glob were not, despite the instruction. I opened files by known or guessed paths, so I sampled about 30 files out of roughly 1,480.
- **Not read:** SQL migrations, integration tests, CI workflows, and most of the Python source (only `zns-chatbot/plugins/__init__.py`).
- **Status:** source only, nothing was run. The git snapshot says the tree is clean, but the known status says working-tree repairs are pending. So what I read may not match the source being repaired.

---

## 1. Implementation breadth: how much required behavior appears in source

The denominator is the required outcomes of stages A–E (plan table `architecture-refactor-plan.md:32-36`, D requirements `:93-252`), the active parity inventory (`parity-current.md:27-54`), and the rollout artifacts (`go-deployment.md`). Each figure is a judgement from sampling, roughly ±10 points.

| Area | Breadth | Evidence and main gaps |
|---|---|---|
| A. Ownership | ~100% | Historically accepted. |
| B. Composition / typed scenarios | ~90–95% | Combined app wires local typed clients through one authorizer (`platform/cmd/zns/app.go:57-68`, `:144-157`, `:193-198`). Residual acceptance is carried into C. |
| C. Coordinators / agent host | ~75–90% | An `agenthost` package exists. D-004's missing lock appears fixed in source (`agenthost/read_store.go:380-403`). **Still open:** D-002 operation discovery, D-003 public status (`DEFERRED.md:43`, `:59`), D-005/D-006/D-007, and C5b workflow/shared projections (`architecture-stage-c-plan.md:236`). The Telegram adapter still holds a raw DB pool and imports `workflow`/`core` (`internal/bot/bot.go:25`, `:32`, `:43`, `:77-84`), so the mid-C condition to move host authority out of Bot (`architecture-stage-c-plan.md:44-46`) is not visibly met. |
| D. Lifecycle / delivery / FIFO / throttling | ~60–80% | **Present:** role admission via advisory locks (`runtimeapp/admission.go:48-77`); maintenance runs inside admitted work (`cmd/zns/main.go:98-104`, `app.go:103-107`); a shared delivery queue with per-lane sequence, head-only admission, cooldown and fairness (`delivery/queue.go:14-139`, `queries/queue.sql:28-31`, `:46-62`); a 429 with unknown scope extends both bot and chat without using the failure budget (`delivery/pacing.go:62-105`); invalid delays are parked (`policy.go:16-28`); ingress positions are committed before model work (`registrationingress/ingress.go:42-93`); allocation orders by registration position first (`passbooking/queue.go:77-85`). **Gaps:** see §3 on writer fencing and pause-clearing; metrics/error classification and shutdown ordering not verified. |
| E. Disposable importer / shared packaging | ~55–70% | The runtime module does not depend on the importer (`platform/go.mod` has no reference; the importer points back via `tools/migrate/go.mod:12`). The image builds only `./cmd/zns` (`platform/Dockerfile:6`). **Open:** classifying temporary receipts vs permanent legacy references, cross-domain identity reconciliation, removal rehearsal (`parity-current.md:35`), and the unresolved `notified_no_more_passes` field (`tools/migrate/passes-contract.md:83`). |
| Active product parity | ~80–90% | Most surfaces have Go implementations (`parity-current.md:29-54`). Remaining: broadcast audience semantics verification (`:32`), full imported-event contract reconciliation (`:33`), agent batch exposure (`:42`). |
| Production rollout artifacts | ~30–45% | A compose file, role init script and runbook exist. The publication workflow still builds the Python image (`production-runtime-contract.md:77-78`). No image digests, proxy attachment or secrets (`go-deployment.md:36-39`, `:172-174`). |

**Overall breadth: about 70–85%.** Uncertainty is high, mainly because of sampling.

## 2. Verified acceptance: what is actually proven

The denominator is the non-skippable final gates: stage acceptance for C, D and E; the full parity matrix under Functional QA; a composed race run; real integrations; import rehearsal and reconciliation; and deployment acceptance.

- **Accepted at stage level:** A and B1 only.
- **Final gates passed: none.**
  - C7 Functional QA is incomplete.
  - D and E have had no stage QA.
  - No composed Linux race run. The only reported full race pass was a pre-fix candidate (`DEFERRED.md:31`, `:104`). The latest guarded race on C7/D5 failed all three D-001 subcases (`DEFERRED.md:106`).
  - No Telegram-like EN/RU mouse/touch Functional QA on the final composition.
  - No real Zitadel, Telegram, model or ASR proof.
  - No import cutover acceptance.
- **Baseline is not green:** 104 failed test events, 47 lint issues, and sqlc drift. The drift is visible in `platform/sqlc.yaml`: timestamp type overrides exist in some packages (`:23-31`, `:107-111`, `:145-149`) but not others.
- **Scoped evidence (useful, not acceptance):** five focused PostgreSQL scenarios passed, the standalone importer ran against all-PostgreSQL in about 50 s, and compile/vet pass.
- **Historical slice acceptances** listed in `parity-current.md` (food, lineup, timetable, credits and others) were against earlier candidates. They need re-verification on the final architecture.

**Acceptance figures:**
- Against final release gates: about 0–10%.
- If historical scoped acceptances are counted as partial credit: about 15–25%, but that credit is expected to lose value as the architecture changes.
- I did not derive these from test counts.

## 3. Release blockers and production readiness

**Architecture and data-integrity blockers**

1. **Writers are not fenced after admission loss.**
   - `admission.go:35-36` says admission "does not fence transactions or effects on any other connection". `lifecycle.go:19` repeats "not a transaction or effect fence".
   - Loss is only noticed by a ping every 1 s with a 2 s timeout (`admission.go:21-22`, `:139-157`). A replacement instance can take the lock as soon as the old session dies.
   - The plan requires owned writers to stop before replacement work starts (`architecture-refactor-plan.md:112-116`).
   - Per-attempt leases exist in some owners (e.g. `announcements.go:81`), but I could not confirm that every writer has one.
2. **A single parked or paused delivery may stop all delivery for the bot.** `Schedule` writes the pause reason at bot level for Parked/Paused outcomes (`pacing.go:84-102`). `ExtendPacing` never clears it (`queries/pacing.sql:12-15`), and candidate selection excludes any paused bot (`queue.sql:57`). I found no resume path in the files I sampled. This needs verification.
3. **Privacy / restricted roles.**
   - The runtime role gets full DML on all `core`, `bot` and `interaction` tables (`deploy/init-roles.sh:25-27`).
   - Bot runs raw SQL on the pool (`bot.go:77-84`). This is consistent with the Code QA2 restricted-role direct-read defect.
   - `CompleteRegistration` writes read results without a lock or revalidation (`read_store.go:421-431`) and relies on reauthorization at read time (`:246-250`).
   - `ReconcileMemory` and `ReconcileHistory` use `LIMIT 100 … SKIP LOCKED` (`:263-267`, `:324-326`), so a single pass can leave rows unreconciled.
   - Code QA3's memory-retirement findings need reproduction.
4. **Open deferrals that must close before final acceptance:** D-001 through D-007 (`DEFERRED.md:29`, `:43`, `:57`, `:69`, `:75`, `:85-87`).
5. **Baseline:** the 104 failures, 47 lint issues and sqlc drift must be triaged to zero, or each given an explicit disposition.

**Release and operations blockers**

6. No Go publication pipeline; images not built, pinned by digest or reviewed. Some base images use mutable tags (`go-deployment.md:36-39`).
7. Reverse proxy, secrets and backup-restore not tested. Roles and grants not re-verified on real PostgreSQL after the final schema (`go-deployment.md:69-73`).
8. No stopped-writer import rehearsal, reconciliation or rollback drill. Once Go has written data, rolling back to Python is not safe (`go-deployment.md:156-163`).
9. The Functional QA stand is not ready. The old Docker stands are deleted.
10. Candidate29 (production startup diagnostics) failed Functional QA. The follow-on fix is unverified (`production-runtime-contract.md:70-73`).

## 4. Remaining effort (8-hour person-days)

| # | Work package | Range (days) |
|---|---|---|
| 1 | Baseline stabilization: triage the 104 failures, fix 47 lint issues, sqlc regeneration and override policy | 3–7 |
| 2 | C closure: D-002/3/5/6/7, remaining D-004 orderings, QA2 role defect, QA3 reproduction, Bot authority extraction, C5b; freeze, then Code QA and Functional QA, with rework | 8–16 |
| 3 | D closure: writer fencing (epoch/lease), pause lifecycle, metrics and error classification, shutdown order, D-001 root cause, composed Linux signal/crash/overlap/throttle/FIFO suite, architecture audit, both QA gates | 8–15 |
| 4 | E closure: receipt/reference classification, full cross-domain import and identity reconciliation, removal rehearsal, imported-state runtime checks, QA | 6–12 |
| 5 | Remaining parity features, then the full EN/RU mouse/touch parity matrix (includes ~1–2 days of stand setup) | 8–18 |
| 6 | Real integrations: Zitadel, test Telegram bot, OpenAI/ASR, Drive sources | 3–7 |
| 7 | Release engineering: pipeline, digests, secrets, proxy, backup/restore, grants | 4–8 |
| 8 | Cutover rehearsal on a production-snapshot copy, go/no-go, cutover, hypercare | 3–6 |
| | **Total** | **43–89 (likely 55–70)** |

- **Biggest uncertainty:** how many P1 defects each fresh QA round finds. The deferral history (seven entries, several P1 privacy issues) suggests one or two repair cycles per stage. Stand availability and real provider access also matter.
- **Critical path:** 1 → 2 (C freeze and QA) → 3 (fencing and composed race, then D QA) → 5 (parity matrix on an imported-state stand) → 6 → 7 → 8. Package 4 can run in parallel after 1, but must finish before 5 runs against imported state. The Functional QA stand gates 2, 3, 5 and 6.
- These are engineering-effort figures, not calendar promises.

## 5. Decisions needed from you

1. Authorize push/PR, changing the publication workflow, and production deployment (all currently unauthorized: `architecture-refactor-plan.md:268`).
2. Choose the fencing mechanism for admission loss.
3. Decide operator procedure for paused/parked delivery.
4. Choose the historical announcement policy (`suppress_historical` or `preserve_source_eligibility`) and a disposition for `notified_no_more_passes`.
5. Decide whether to keep the separate `bot`/`api` modes (`architecture-stage-c-plan.md:90`).
6. Set credits mode at launch (legacy vs credits).
7. Set the cutover window, source freeze, and who decides which data is authoritative after Go writes.
8. Provide test Telegram, Zitadel and provider credentials.
9. Decide which residual deferrals, if any, are acceptable at release. By its own text, D-001 must close.

## 6. Release gates that cannot be skipped

- A green composed baseline, including a Linux race run with unchanged D-001 assertions.
- Fresh independent Code QA and source-blind Functional QA for C, D and E, including the authorization/privacy guards (these cannot be skipped).
- The full EN/RU parity matrix under Functional QA.
- Real Zitadel, Telegram and provider acceptance.
- Stopped-writer import rehearsal with reconciliation and a backup-restore drill.
- Reviewed release images pinned by digest, and deployment acceptance on the final Linux composition.

## 7. Contradictions, assumptions and missing evidence

- **Stale plan text:** `architecture-refactor-plan.md:84-85` says Stage C "has not started", while `architecture-stage-c-plan.md:228-236` describes active C implementation.
- **D-004 description is outdated:** DEFERRED says completion reads without `FOR UPDATE`, but the current source has it (`read_store.go:388`). The fix exists in source but is not validated, and two further orderings were left uncovered (`DEFERRED.md:94`).
- **Pending repairs vs clean tree:** the known status mentions pending working-tree repairs, but the git status is clean. The repairs probably live in `qa.local` trees I was not allowed to read.
- **Scope note:** `parity-current.md:35` separates scoped import passes from the full import contract. I followed that and did not infer full import coverage.
- **Not verified:** migrations, accounting-role grants, integration test content, workflow files, shutdown/telemetry ordering (`architecture-refactor-plan.md:109` flags DB close happening before telemetry flush), and metrics coverage.
