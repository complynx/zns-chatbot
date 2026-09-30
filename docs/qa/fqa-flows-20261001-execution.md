# Functional QA flows: frozen scoped execution

Verdict: supported manual UI cases below passed their stated checks. Full Stage C and the migration are NOT ACCEPTED: required coverage remains blocked. No executed product requirement failure was established in this batch.

Freeze epoch: `ce427ca56ff9f127eba55321047c64a5e9204683`.
Public image binding: `synthetic-qa-zns-fqa-app@sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`.
Assigned stand: `synthetic-qa-zns-fqa-flows`, UI `http://127.0.0.1:58403/`.
Source: original acceptance requirements and `docs/qa/fqa-lead-2026-10-01-public-handoff.md`, including its public model fixture contract. No source, implementation diff, engineering report, prior QA findings, or other reviewer's evidence was inspected.

## Method and evidence

Executed isolated headless Edge with the handoff's existing Playwright package, fresh browser contexts, real DOM clicks, file input, dialog/iframe interaction, screenshots, and visible transcript readbacks. Browsers were closed in `finally`. No personal profiles, rebuild, restart, reseed, Git operation, production operation, or product edit.

Evidence is in `qa.local/fqa-flows-reviewer/`. `inspect.cjs`, `run.cjs`, `manual.cjs`, `extended.cjs`, `mini-inspect.cjs`, and `final-ui.cjs` contain executed actions. Numbered PNG plus JSON/TXT snapshots record UI observations. `state-alice.json`, `final-state-alice.json`, and `network-extended.json` are public black-box supplementary evidence. Earlier snapshots can capture delivery before later polling renders a response; conclusions below use subsequent final observations, not an assumed immediate result.

## Executed observations

| Case | Result and exact extent | Evidence |
| --- | --- | --- |
| Manual selection and confirmation | PASS: Alice's existing massage draft v1 changed to transfer draft v2 by clicking the old visible service button, then confirmed to booked v3. The original card was edited and confirm/select controls disappeared after booking. An unrelated text between selection and confirmation returned the documented unavailable-assistant fallback; booking draft remained coherent. This is manual/fallback continuity, not successful agent registration. | `06-alice-transfer.json`, `07-unrelated.json`, `08-confirm.json`, `12-alice-return.json` |
| EN/RU UI locale | PASS for exercised controls: `/language` exposed EN/RU, saved EN, changed current booking/media/order controls to EN, then saved RU and refreshed those controls to RU. Separate browser context/reload retained RU and booked state. Seeded service titles remained Russian; their localized-content contract was not supplied. Language refresh is asynchronous: immediate snapshots showed old cards, later snapshots showed refreshed cards. No maximum refresh SLA was supplied. | `05-alice-en.json`, `07-unrelated.json`, `15-ru-refresh.json`, `16-upload.json`, `19-session-persistence.json` |
| User isolation and denied callback | PASS for exercised manual surface: Boris and Visitor showed their own unselected workflow v0 without Alice's order/upload. Visitor clicking massage yielded `forbidden` and stayed at v0. This does not establish live revocation, event-specific role policy, or private-memory isolation. | `10-boris.json`, `11-visitor.json`, `14-visitor-denied.json`, `18-boris-isolation.json` |
| Document upload/download | PASS: clicking `readiness.txt` produced a browser download whose contents were `Synthetic FQA readiness document.` Alice uploaded `fqa-private-marker.txt` with caption, and a visible media-intent card appeared. Own file bytes read back matched `FQA-ALICE-PRIVATE-20261001`. Navigating the same file resource with synthetic user 202 yielded a 404 page without file contents. This tests the harness's per-user resource boundary, not production Telegram authentication. | `download-readiness.txt`, `16-upload.json`, `17-upload-unrelated.json`, `download-private.txt`, `27-cross-user-file.txt`, `27-cross-user-file.png` |
| Mini App | PASS: actual dialog/iframe opened from Alice's order. Filled synthetic first/last name, selected transfer, calculated 65 BYN, saved, saw success text, and observed chat order v2 with 65 BYN and the same name/service. Reopening restored saved inputs. Mini App EN selector changed visible labels to EN. | `21-miniapp.png`, `22-mini-save.png`, `23-after-mini-save.txt`, `24-mini-en.txt`, `24-mini-en.png` |
| Stale order card | PASS: two browser pages initially showed order v2. Held only the stale page's UI state polling with browser request interception; current page added Preparty, yielding order v3 and 100 BYN. Clicking the old page's still-visible Add Preparty button yielded `stale_version`; reload showed order v3 and 100 BYN, without a duplicate charge. No app/provider state was fabricated. | `25-order-change.txt`, `26-stale-result.txt`, `final-ui.cjs` |
| Pending profile form | PASS for manual command escape: clicked Set full name on the profile card, saw text explicitly allowing another question, then clicked `/orders`. Orders navigation succeeded while full name remained unset. Natural free-text interpretation and cancellation/change-intent semantics remain unproven. | `28-name-pending.txt`, `29-pending-orders.txt` |
| Deterministic profile capability | OBSERVED SUCCESS, bounded capability only: after selecting RU via UI, installed the exact documented profile proposal with `expect.language=ru`. Installation auto-enqueued update 27. UI displayed the profile/full-name/dance-role card. Public model readback reported accepted=1, rejected=0, next_turn=1, total=1. This establishes this one fixture route, not model understanding, agent language selection, successful agent registration, or wider tool orchestration. | `fixture-install.json`, `fixture-state.json`, `20-model-capability.json` |

Callback acknowledgement is BLOCKED: button-triggered business outcomes and message edits were observed, but neither visible `#status` nor supplied `/lab/state` exposed an explicit callback acknowledgement. Card updates alone do not prove Telegram `answerCallbackQuery`. Lead was asked for a public observation contract; no acknowledgement PASS is inferred.

## Original plan coverage remains tracked

| Plan | Observed subset | Remaining status |
| --- | --- | --- |
| F01 | Manual selection/confirmation, fallback continuity; one agent-fixture profile route | BLOCKED: successful manual→agent→manual registration/knowledge operations and real-provider behavior |
| F02 | Media card plus manual pending-profile escape to orders; unrelated text fallback did not alter booking | BLOCKED: natural free-text/media intent, explicit cancellation/change-event, full pending-intent semantics |
| F03 | EN/RU manual controls, changed-locale refresh, persisted locale and state | BLOCKED: independent agent question language and real language switching |
| F04 | EN/RU selector only | BLOCKED: be/by/uk/ua/pl controls and controlled fallback catalog gaps |
| F05 | Synthetic user transcript/upload isolation and cross-user file denial | BLOCKED: private memory/knowledge, model-created identity/binding attempts, deleted/revoked contexts |
| F06 | Static Visitor booking denial | BLOCKED: ordinary/payment/global-admin-without-payment-role matrix, event-A/B, live roles/consent, delayed call denial, discovery/provider-input evidence; callback acknowledgement proof |
| F07 | Edited order card and stale order rejection | BLOCKED: knowledge mutation/deletion and authorized history freshness |
| F08 | Unavailable-assistant fallback followed by successful manual confirmation | BLOCKED: budgets, script limits, interrupted/delayed tool work, cancellation/replay and lifecycle recovery |
| F09 | No rank claim | BLOCKED: first event-specific sales-open check/rank/deadline/replay controls |
| F10 | Plan corrected: retained pre-opening intents preserve original initiation order; no pre-opening pass acquisition | BLOCKED: controlled opening and competing original-ingress/delivery/replay evidence |
| F11 | No expiry claim | BLOCKED: documented registration expiry/time/rank/draft controls, default ten-minute verification and tail movement |
| F12 | Visitor denial; transfer availability visibly dropped from 2 to 1 after booking | BLOCKED: concurrent last-place, pair/role/tier constraints and exact seeded rules |
| F13 | State/locale survived reload and new browser contexts; Mini App save reopened coherently | BLOCKED: process interruption/durable recovery, cancellation persistence and full history contract |

## Failed / blocked / untested

- Failed: none established among the bounded checks executed above.
- Blocked: missing scoped controls/capabilities listed in the coverage table and explicit callback acknowledgement evidence.
- Untested: other media formats, payment completion/refund lifecycle, contact/forward/group/forum semantics, import, database/process failure, successful wider model orchestration, real providers, complete Python parity. These are not passed by the scoped UI checks.

The reviewer has finished this manual batch and releases scenario-writer ownership of the stand to the lead. State is left intact: Alice's simple transfer booking is v3/booked; festival order `CKR2RFAY2WQ6O4MJD2FUM7LJYF` is v3/unpaid, total 100 BYN, FQA Alice Synthetic with transfer and Preparty; UI locale RU; private marker upload and profile pending hint remain. Boris is unselected; Visitor is unselected after denied massage. No reset is requested during this freeze.
