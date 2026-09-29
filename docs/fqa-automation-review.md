# Functional QA automation review

Research date: 2026-09-27. This is an automation proposal, not an acceptance review. No stand, database, container, product or harness was changed. The composed-runtime stand remains owned by `composed_runtime_functional`; its current gate is single-lead and awaits immutable candidate images ([current scope](../qa.local/composed-runtime-functional/current-acceptance-scope.md), lines 3–12).

The main opportunity is to consolidate existing transport and evidence helpers, then make coverage explicit. Do not build another general test framework. Preserve independent Code QA and fresh source-blind Functional QA. Developer regression results are useful inputs to development, not substitutes for acceptance.

## What already exists

| Capability | Existing evidence | Reuse or gap |
| --- | --- | --- |
| Public-client and fixture lifecycle | [FQA README](../platform/scripts/fqa/README.md), lines 51–93; [run.mjs](../platform/scripts/fqa/run.mjs) | Journals creation intent before mutation, preserves operation keys, resumes ambiguous creation, cleans only owned orders, preserves paid evidence. Do not duplicate this. Current manifest is order-specific, not a general scenario matrix. |
| Actual browser transport | [browser.mjs](../platform/scripts/fqa/browser.mjs), lines 1–100; README lines 125–137 | Playwright click/tap, exact message IDs, input response + processed cursor, downloads, screenshot/trace/page errors. Upload and Mini App completion still need explicit predicates. Three fixed actors and one DOM/transport shape limit reuse. |
| Polling and assertions | [client.mjs](../platform/scripts/fqa/client.mjs), lines 52–67; [tests/fqa.mjs](../platform/tests/fqa.mjs) | Bounded predicate polling with last observation; Node strict assertions. The kit example covers mouse/touch in RU, not the complete EN/RU matrix. |
| Fixture and restart controls | [fixture.mjs](../platform/scripts/fqa/fixture.mjs), lines 51–108 | Allowlisted controls, no inherited fixture settings, restart without rebuild. Compose files and service names are tied to the original sandbox; do not point these controls at another project implicitly. |
| Identity setup | [independent setup plan](../qa.local/imported-runtime-fresh-functional/SETUP-AND-PLAN.md), lines 5–16; [identity setup launcher](../qa.local/identity-v2-fqa/setup-owned.ps1) | Real local Zitadel, synthetic exact owner mappings and frozen image checks already exist in QA-owned infrastructure. Seeding is not onboarding proof. Avoid rebuilding an IdP provisioning system. |
| Scoped faults and observed barriers | [private Telegram adapter](../qa.local/imported-runtime-fresh-functional/telegram.cjs), lines 40–48; [provider adapter](../qa.local/imported-runtime-fresh-functional/provider.cjs), lines 11–26 | Telegram method/recipient selection, hold, after-effect disconnect/status, provider input-keyed controls already exist. The kit README lines 189–191 describes these as future gaps; the gap is shared availability/documentation, not complete absence. Provider holds need an observed boundary and a release record. |
| Replay/crash evidence | [knowledge report](../qa.local/knowledge-private-fqa/report.md), lines 33–35 | Exact update replay; observed successful API response held before SIGKILL; persisted version checked after restart. Reuse transport primitives, keep scenario expectations independent. |
| Scheduling | [quality.mjs](../platform/scripts/quality.mjs), lines 142–153; [global coordination](../qa.local/composed-runtime-functional/global-fqa-coordination.md) | Existing browser scripts run serially. A written lead/actor/barrier plan exists; current stand is not running parallel acceptance. No machine-readable general scenario catalog was found in the inspected tracked docs/kit. Scenarios are spread across contracts, neutral scopes, scripts and reports. |
| Independent artifact inspection | FQA README lines 174–187; [full-history report](../qa.local/full-history-functional/report.md), lines 27–41 | ZIP/XML XLSX inspection independent of the product writer; exact raw-input Unicode/body/hash reconciliation. Preserve these independent oracles. |

The inspected scripts are small Node programs using installed Playwright `1.62.1`, Node assertions/test runner and standard libraries ([package.json](../platform/package.json)). Reuse these pinned dependencies initially. Adopting Playwright Test later could supply projects/reporters/fixtures, but adding it now creates dependency acceptance work and a second runner without first resolving shared-state ownership.

## Measured evidence and limits

No reliable end-to-end elapsed-time ledger was found in the sampled reports. File modification times are not execution durations. Therefore no measured hours-saved or percentage speedup is claimed.

There are concrete repeat costs:

- Knowledge QA discarded and reran an incomplete suite after its provider confused summary and Plan requests; another harness error split a UTF-16 surrogate ([report](../qa.local/knowledge-private-fqa/report.md), line 44).
- Modern orders QA initially rewrote every proxy path to `/plan`; missing path/status capture prevented exact historical diagnosis ([report](../qa.local/modern-orders-fqa/report.md), line 27).
- Identity QA required readiness of 10 seconds rather than 2, recovered a proxy/Docker DNS setup problem, and repaired a callback that consumed authorization state on prefetch ([report](../qa.local/identity-v2-fqa/report.md), lines 44–45).
- Knowledge's eight-decision matrix first sampled before the 400ms UI refresh, then used 1500ms waits ([report](../qa.local/knowledge-private-fqa/report.md), lines 54–58). In that script eight explicit 1500ms waits alone impose 12 seconds. Predicate completion can remove unnecessary delay and avoid premature sampling; it cannot promise zero latency or a particular total saving.
- The modern orders 49 retries and identity more-than-100 attempts were product-failure evidence, not setup time to optimize away. Keep such failure evidence; use bounded diagnostic stopping rules instead of silently retrying until green.

## Prioritized changes

Effort below is an engineering estimate including focused tests and review, not observed duration. Savings describe eliminated work; quantify wall-clock gains after instrumenting two comparable frozen runs.

| Priority | Concrete change | Effort estimate | Expected benefit and interference risk |
| --- | --- | --- | --- |
| 1 | Add a read-only scenario/evidence catalog and validator, with candidate identity, required cells, status, evidence paths, phase timings and resource/barrier declarations. Seed only the current B scope. | 0.5–1.5 days | Removes manual reconciliation and catches missing EN/RU/input or restart evidence before handoff. No defensible time estimate yet. Very low interference: offline files only. |
| 2 | Extend the existing browser helper through an explicit stand adapter and common observed-completion helpers. Support uploads/Mini App saves, predicate-based card refresh, traces on failure; never infer success from message count or sleep. | 1–3 days | Removes repeated selector/response/wait code and known refresh-related reruns. Seconds per matrix plus avoided reruns; larger saving unmeasured. Moderate risk: changes shared helpers, so pilot on a new disposable stand only. |
| 3 | Promote already-built setup-only infrastructure into one documented stand descriptor/preflight and scoped lifecycle command. Describe project, files, immutable images, actors, endpoint/auth mode, schema/import manifest, capabilities and owned resources. | 2–4 days | Reduces repeated private Compose/identity/proxy preparation and accidental endpoint mismatch. Likely higher absolute saving than faster clicks, but no usable baseline exists. Moderate risk; preserve the active stand and its data. |
| 4 | Consolidate targeted provider/Telegram controls and a small observed-barrier client from the existing adapters. Record arm → observed request/effect → mutation/restart → release → final state; exact replay IDs; restore/cleanup state journal. | 2–4 days | Removes repeated bespoke proxies and timing races; permits safe independent scenarios. Higher risk: external adapters are test oracles. Contract-test method/path/body forwarding, request-shape discrimination and recipient isolation first. |
| 5 | Introduce affected-scope selection and lead-coordinated isolated workers using catalog metadata; retain final full gate. Start with a printed schedule, then bounded execution only if useful. | 1–2 days after 1–4 | Reduces unnecessary interim repetitions and overlaps independent browser work. Speedup limited by shared barriers, DB/identity and the two heavy lanes. No throughput promise before measurement. |

Priority 1 is the safest immediate implementation while migration continues. Priority 2 likely delivers the first visible runtime improvement. Priorities 3–4 have the strongest evidence for avoiding whole-suite reruns, but should be adopted after the current reviewer releases the stand.

### First small implementation ownership

Assign one builder only:

- `platform/scripts/fqa/coverage.mjs`: pure catalog/result validation and coverage summary; no Docker, HTTP, DB or browser calls.
- `platform/scripts/fqa/coverage.test.mjs`: Node test cases with temporary local fixtures.
- `platform/scripts/fqa/coverage-cli.mjs`: explicit input files/output summary; nonzero on incomplete required evidence.
- `docs/fqa-scenarios.md`: neutral requirement IDs and usage; no previous findings, implementation hints or copied acceptance verdicts.

Do not edit `package.json`, `quality.mjs`, existing kit modules, active QA evidence, or product files for this pilot. Run directly with Node. A future integration decision can wire it into gates after adoption. Research ownership is only this report.

Tests should prove rejection of duplicate/unknown scenario IDs, missing cells, skipped/blocked treated as incomplete, candidate mismatch, absent evidence and contradictory pass/error status; a valid complete synthetic matrix should pass. Do not trust `passed: true` alone. This validator proves report completeness, not semantic correctness of evidence; a fresh reviewer remains responsible for that judgment.

## Scenario matrix and example

Use stable requirement IDs, not filenames as the scope model. Each case declares actors/resources, input modes, locale cells, transport, setup preconditions, expected externally visible outcomes, exclusive barriers, and required evidence. Store results separately from the neutral catalog. Statuses: pending, passed, failed, blocked, skipped; only passed with required evidence closes a cell.

| Family | Deterministic baseline | Independent exploratory emphasis | Exclusive operations |
| --- | --- | --- | --- |
| Orders across channels | manual → queued provider/external Sobek → Mini App → manual; exact contents/price/version; stale/foreign/replay; native upload/download | Change intent mid-draft, choose another order, navigate old cards, unusual Unicode/large catalogs | Shared capacity and authorization mutations |
| Permission/discovery/privacy | ordinary, event-A admin, global admin without payment grant, event-B denial; forbidden names absent from actual provider input/help; revoked execution and cached result denial | Conflicting context and unexpected tool combinations | Grant/revoke; history/memory deletion; authority outages |
| Import/runtime continuity | exact original owner/body/time/receipt bytes; omissions distinct from absence; runtime after importer removal | Cross-domain imported/manual interactions | Import/clone/schema preparation before freeze; restart |
| Durability/failures | exact update replay, hold after observed effect, process restart twice, current permissions, no duplicate effects or restored spend | Late completion, interleaved users, stale UI after retries | Global restarts; non-targeted transport/provider faults |
| UI/localization | EN mouse, EN touch, RU mouse, RU touch; messages/buttons/edits/upload; current card and real DOM event evidence | Readability, misleading status, discoverability, intent transitions | None if fixture state is disjoint |

Example `orders.cross-channel.current-owner`: a reviewer starts with a distinct owner/event/order fixture; sends and clicks through Telegram UI; queues a deterministic plan with a unique input key; observes actual Sobek execution and its result; changes the selected order through the Mini App; returns to the original message and checks current controls, exact price/version and stale-command refusal. Save correlated update/message/order IDs, before/after DOM and authoritative state, screenshot/trace, candidate hash and cell. Repeat required locale/input cells using distinct mutable fixtures. A restart variant enters an explicit lead-owned barrier and verifies final-format persistence. API setup/assertions supplement the rendered journey.

Do not multiply every business edge by every locale mechanically. The lead assigns which UI journeys require four cells and which data invariants are locale-independent; all required cells still run. No silent pairwise reduction of the required EN/RU mouse/touch gate.

## Scheduling, independence and rollout

1. **Now:** implement offline coverage/timing pilot; document available stand capabilities versus the stale kit roadmap. Leave current acceptance and stand ownership unchanged. Record setup/run/evidence/cleanup durations separately, harness errors separately from product defects.
2. **Next released candidate:** reuse the existing driver for one four-cell order journey, with exact predicates. Run its helper contract tests and a fresh independent functional pilot. Compare with the prior procedure on equivalent fixtures; retain failed attempts in metrics.
3. **After pilot:** consolidate descriptor and controls without copying product scenarios into acceptance. Setup-only reusable tools are allowed; fresh reviewers derive expectations from original contracts and live interfaces. Give acceptance agents neutral contracts/access only, never this research report or historical findings.
4. **Broader B–E gates:** one FQA lead assigns distinct owners/events/provider prefixes and artifact directories. Run independent slices concurrently only after confirming separate mutable state. Pause workers at permission/restart/global-fault barriers; record in-flight IDs, runtime identity and release. No rebuild while accepting a frozen candidate.
5. **Final:** run the complete required synthetic matrix on the integrated frozen candidate, then bounded real-model/ASR and designated Telegram account/bot checks under existing authorization. Physical touch remains separate from browser touch emulation. No automation may label an unavailable integration passed.

Affected runs accelerate development but do not change acceptance rules. Map changes to public capabilities: a renderer change selects UI cells; authorization/history/cache changes select all relevant privacy/discovery and resume cases; schema/import changes select reconciliation and composed runtime. Cross-cutting or unknown impact defaults to broader coverage. After substantive fixes obtain fresh affected Code/Functional reviews; the final integrated full gate still runs. The implementation agent must not accept its own regression suite.

Use deterministic scripted regression for stable invariants, generative agent exploration for new sequences and ambiguous free text/media, and real integrations/human review for actual Telegram/widget/physical touch/provider-quality behavior. Generative agents should produce reproducible steps and minimized fixtures; they are not a replacement for deterministic replay or independent acceptance. Real-model spend is explicitly bounded and recorded after synthetic tests.

Metrics for adoption: required-cell completeness; percentage of cases with candidate/fixture/evidence identity; setup and execution median/p95 once enough comparable runs exist; harness-failure reruns; fixed-sleep count; time waiting at exclusive barriers; cleanup failures; unexplained flaky passes. Initial acceptance: no missing required cells accepted, no candidate mismatch accepted, no hidden retry of mutations, preserved independent reviewers, and demonstrated replayable evidence for the pilot. Numeric speed targets should follow the baseline, not precede it.

No new decision from Daniel is needed to start the offline pilot. Root must assign its file owner and keep migration/QA heavy lanes prioritized. Exact future real-model request/spend caps can be recorded when that gate is scheduled; they do not block synthetic automation.

Written by fqa_automation_research (gpt-6-sol/Codex)
on behalf of Daniel Drizhuk
