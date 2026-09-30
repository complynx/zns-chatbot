# Independent Opus readiness assessment — 2026-09-30

A fresh read-only Claude process assessed current source and the explicitly supplied test/check logs. No prior readiness estimates, parent estimate, progress reports or Code QA findings were supplied. This assessment does not increment the development rotation or Code QA counter.

Execution evidence: exit 0; JSON subtype success; is_error false; canonical model `claude-opus-5-5`, provider `firstParty`; 295218 ms; 50 turns. Effort was requested explicitly with `--effort medium`; response metadata does not separately report effective effort. Only Read was used. Local prompt, stream and compact metadata are retained as `qa.local/go-resume-20260929/opus-readiness-20260930.*`.

Interpretation limits: independent estimate, not acceptance proof. The assessor sampled the full test log and source, did not compare all candidate hashes, and ran no checks. It received native-pg-all-3 and focused-1/2 evidence; later gates and concurrent repairs were outside its evidence window. Some release-status statements rely on requirement documents and may be stale. Its source-presence, partial-acceptance and release-gate percentages use different denominators. Treat owner-decision suggestions as suggestions; existing requirements and authorization remain authoritative. No engineering requirement is waived by this report. The assessor also reports ignoring unexpected instruction-like text in a tool result; this claim is retained as its own observation, not independently validated by the launcher.

The independent response follows verbatim.

---
# ZNS chatbot: Go/PostgreSQL migration readiness assessment (read-only, as of 2026-09-30)

**Bottom line:** No candidate has passed. The last full PostgreSQL integration run failed, the two later targeted re-runs both failed, and the current test source has changed again since the last run. A lot of the design is built in source, but almost none of it is proven, and nothing is ready for production. I estimate **about 85–190 person-days** of work remain.

## 0. How I worked, and what I couldn't see

- **What I read:** only the Read tool, the four requirement documents (for scope only), selected source files, and the evidence files you allowed (`native-pg-all-3.*`, `resume-repairs-focused-1/2.*`, `composition-compile-5`, `lint-all-7`, `sqlc-diff-5`). I didn't read any prior estimates, progress files or QA findings.
- **Sampling limit:** the full-run log has 30,587 lines and I couldn't search it. I read its end and some windows, not every line, so it may contain failures I didn't see.
- **File inventory:** I used the hash manifest (`native-pg-all-3-source.json`) only to list files. I didn't compare hashes, so I can't say which current files match the tested snapshot.
- **Source changes since testing:** the source of `TestPassPlanDeliveryRetryAndRender` has moved again since the last run.
  - The last run reported failures at `:204 via :138` and `:182`.
  - The current file has that fence check at `pass_plan_recovery_test.go:139` (inside `assertPassPlanTerminal`, called from `:212`) and the "invalid saved turn" check at `:180`.
  - So the current edits are untested. Current source is not a tested candidate.
- **Injected instruction:** one tool result contained text styled as a system instruction to "enter plan mode", write a plan file and launch agents. It didn't come from you and it conflicts with your brief, so I ignored it. That same result also held copies of reads I never made, so I read the test file myself.
- **Not checked at all:** real Telegram, Zitadel or OpenAI; Linux Docker runtime; the importer against real data; how the running system performs; and any independent QA.

## 1. Implementation breadth: what exists in source

**Denominator:** 20 required scope items, weighted equally. They are Stages A–E, the architecture elements you listed, the 7 remaining parity surfaces, the owner-confirmed audit items AUD-16/23/24/56/67, and packaging plus rollout.

**Estimate: about 60–72% of items are present in source (uncertainty ±10 points).** This counts code that exists, not code that works.

**Present in source:**
- **Ownership and module layout:** separate packages for credits, orders, pass allocation and booking, registration, knowledge, conversation and others. All of them compile (`composition-compile-5.exit = 0`). Lint is clean (`lint-all-7`: "0 issues") and the generated database code matches the queries (`sqlc-diff-5` exit 0, empty diff).
- **Typed in-process composition:** mostly done. `cmd/zns/app.go` wires `appservices.NewServices`, the local bot delivery, history, knowledge and derived stores, and in-process history, knowledge, orders and registration inside the combined client.
- **Runtime admission and lifecycle:**
  - `runtimeapp/admission.go`: a Postgres advisory lock held on a dedicated connection, a 1-second ping with a 2-second timeout, and `ErrLost` when the lock is lost.
  - `runtimeapp/lifecycle.go`: owned work is cancelled when admission is lost.
  - `main.go`: telemetry is flushed before the database is closed.
  - `runAppServers` waits for the bot worker before closing resources.
- **Durable delivery:** `delivery/queries/queue.sql` and `delivery/policy.go` cover:
  - lanes with a sequence number, where only the head of a lane can be sent (no overtaking);
  - bot-wide and per-chat pacing;
  - persisted retry deadlines;
  - parking of delays that can't be represented;
  - a 3:1 interactive-to-background fairness grant.
- **Registration ordering:** `registrationingress/ingress.go` takes a global transaction lock (`pg_advisory_xact_lock(782619)`) inside the inbox transaction.
- **Importer:** a separate module (`tools/migrate`, which points at the platform through a `replace` directive; the platform doesn't depend on it). It has plan, apply and reconcile steps, digest-bound resolutions (`passes-contract.md`), and no importer service in compose.
- **Production packaging:** `platform/deploy/compose.yaml` is hardened: read-only containers, all capabilities dropped, internal networks, isolated decoders with no network, and database role separation. Production config validation is described in `docs/production-runtime-contract.md`.

**Partial or missing:**
- **Transaction fencing:** `admission.go` says it "does not fence transactions or effects on any other connection". This falls short of the Stage D requirement that writers stop before a replacement takes over.
- **HTTP loopback still in use:** `app.go` builds the combined client with an HTTP base URL, and the `bot` command still uses `appclient.Client` over HTTP (`main.go`). The `bot.Bot` struct is still very large.
- **Owner-confirmed items:** AUD-16, AUD-23, AUD-24 and AUD-56 are recorded as pending implementation in `architecture-refactor-plan.md`. I found no source that clearly implements them. I didn't inspect every relevant file, so this is a gap in my evidence, not proof of absence.
- **Parity surfaces:** browser consent and session, legacy food (callbacks, `/menu`, CSV), broadcast input and audience semantics, registration announcement fields, Drive document refresh, and domain migration with identity reconciliation. `docs/parity-current.md` lists these as remaining.
- **Publication workflow:** the root workflow still builds the Python image (`production-runtime-contract.md`).
- **Importer scope:** `passes-contract.md` covers passes in detail. Complete import across all domains, and its reconciliation, isn't shown.

## 2. Verified acceptance: what has been proven

**Release level: 0%.** The denominator is the release acceptance gates in §4; none has passed. Evidence:
- `native-pg-all-3.exit = 1`. The integration package failed after 1441.5 s. Failures I saw:
  - `TestScriptProfileSemanticWritesLanguagesPrivacyAndReplay/en` and `/ru`: "no rows in result set" (`profile_bot_test.go:22`, `script_profile_mutations_test.go:81`).
  - `TestSavedKnowledgeReceiptCompletesOriginalSources`: 7 of 8 subtests fail with "delivery identity or pacing settings missing" (`saved_knowledge_attachment_recovery_test.go:47/110/129`). This looks like the test setup wasn't updated after the delivery pacing change, but I haven't confirmed that.
  - Because of the sampling limit, there may be more failures.
- `resume-repairs-focused-1.exit = 1`:
  - `TelegramFailureAndPersistence` failed (`platform_test.go:435`).
  - `ScriptPassPaymentGenerationAndExecutionACL/replaced` and `/revoked` failed (`script_pass_binding_test.go:168`).
  - `PassPlanDeliveryRetryAndRender/revoked` and `/missing_authority` failed.
- `resume-repairs-focused-2.exit = 1`:
  - `/revoked` still fails ("Should be zero, but was 1": revoked private text is still in the archive).
  - `/missing_authority` still fails ("invalid saved turn").
  - The ACL test wasn't re-run, so its status is unknown.
- `TelegramFailureAndPersistence` failed in run 1 and passed in run 2. That could be a fix or a flaky test; the evidence doesn't say which.

**Component level: about 20–35% of the ~35 feature and architecture surfaces have passing targeted tests on some candidate (low confidence).** Examples that passed in the targeted runs:
- registration receipts;
- late-exposure and render views for the pass plan;
- stale buttons in manual agent continuation;
- Zitadel reminder retries;
- the exact scope of script pass payment receipts.

None of these results is tied to the current source.

Compile, lint and sqlc are green, but they prove no behaviour. The compile log shows "[no tests to run]".

## 3. Production readiness

**About 0–5%. The denominator is the production gates in §4 (G8–G12). Only the design and compose artifacts exist; no gate has passed.**
- No production-shaped rehearsal.
- No reviewed deployment artifact.
- The publication workflow still builds the Python image.
- Release digests, proxy attachment, secrets and production data approval are all still unfilled (`docs/go-deployment.md`).
- Rollback is unsafe once Go has written data (`go-deployment.md`), so cutover is effectively one-way.
- `passes-contract.md` states: "Independent QA is pending. No production import has run."
- Pushing, opening a PR and deploying are not authorised (`architecture-refactor-plan.md`).

## 4. Release gates

| # | Gate | Status |
|---|---|---|
| G1 | A frozen candidate commit; full native-PG run green; all logs read | Failed |
| G2 | Stage C: typed composition done, or the remaining HTTP loopback accepted; architecture audit | Not done |
| G3 | Stage D: fencing decision; AUD-23 fail-fast plus supervisor restart; signal, crash, drain and overlap tests, including admission-session loss where writers must stop before a replacement | Not done |
| G4 | Throttling and ordering acceptance: persisted retry deadlines, ambiguous 429s, partial batches, lanes that can't be overtaken, 10-minute registration retention | Source only |
| G5 | AUD-16, AUD-24, AUD-56 implemented and tested | Pending |
| G6 | All 7 parity gaps closed or formally retired; all implemented-but-unaccepted surfaces accepted | Not done |
| G7 | Independent QA and full parity matrix | Not done |
| G8 | Importer: full plan/apply/reconcile rehearsal on an approved copy of production data, with stopped-writer cutover | Not done |
| G9 | Real Telegram, Zitadel and model-provider acceptance on Linux Docker | Not done |
| G10 | Go publication workflow, pinned image digests, secrets, proxy, runbook | Not done |
| G11 | AUD-67: pending Python branches checked and rebased onto main | Not done |
| G12 | Owner authorisation for push, PR, deployment and cutover | Not given |

## 5. Remaining effort (8-hour person-days)

| Work item | Person-days | Depends on |
|---|---|---|
| Make the current candidate green (the failures above, the ACL recheck, the flaky Telegram test, a full clean run) | 3–8 | — |
| Finish Stage C composition, plus audit | 5–12 | Green candidate |
| Stage D: fencing, fail-fast, lifecycle tests, throttling and ordering acceptance, audit | 8–18 | Stage C; fencing decision |
| AUD-16, AUD-24, AUD-56 | 4–9 | Green candidate |
| The 7 parity gaps | 20–45 | Owner scope decisions |
| Accept the ~14 implemented-but-unaccepted surfaces | 10–25 | Green candidate |
| Stage E importer across all domains, rehearsal, reconcile | 10–22 | Data approval; stable schema |
| Real integrations on Linux Docker | 5–12 | Credentials |
| Packaging, publication workflow, runbook | 4–9 | Can run in parallel |
| AUD-67 Python branch check and rebase | 2–6 | Late, before final QA |
| Independent QA and parity matrix | 8–15 | All of the above |
| Cutover and immediate post-launch support | 3–6 | All gates |
| **Total** | **about 85–190** | |

- **Critical path:** green candidate → Stage C → Stage D → parity gaps and acceptance → importer rehearsal → real integrations → rebase → independent QA → cutover.
- **Biggest uncertainties:**
  - the parity-gap work, especially identity reconciliation and broadcast audience rules;
  - whether the fencing decision needs a database-level epoch or fence mechanism;
  - how many problems the importer rehearsal turns up;
  - failures hidden in parts of the full log I didn't read;
  - rework caused by the Python rebase.

## 6. Decisions for you as owner

These are yours to make, as opposed to routine engineering choices:
1. Is advisory-lock admission without transaction fencing acceptable, or must writers be fenced (for example with an epoch column)?
2. Cutover downtime window, and formal acceptance that rollback isn't possible after Go writes.
3. Approval to use production data for the rehearsal and import, and the retention rules for that data.
4. For each parity gap (legacy food and CSV, Drive refresh, browser consent, broadcast audience semantics): build it, change it, or retire it.
5. Policy for reconciling identities during the domain migration.
6. Release digests, proxy attachment, secrets and credentials for real-integration tests.
7. Which pending Python branches to merge (AUD-67).
8. Authorisation for push, PR and deployment.

The routine engineering choices can be left to the team: fixing the test setup and failing tests, removing the HTTP loopback, test design, workflow YAML, and fairness tuning.

## 7. Contradictions and caveats

- `docs/parity-current.md` and `passes-contract.md` claim status I can't back with current evidence. I treated them as scope only.
- The Telegram failure test flipped from fail to pass with no clear fix.
- The pass-plan test was edited between the two targeted runs and again afterwards, so every result is out of date against the current source.
- The compile check proves the code builds, not that it behaves.
- The report contains no private payloads or secrets.

---

Written by code_qa_03_opus (claude-opus-5-5/Claude CLI assessment; Codex launcher)
on behalf of Daniel Drizhuk

