# Independent Functional Senior QA B: scoped recovery UI report

Epoch: `ce427ca56ff9f127eba55321047c64a5e9204683`.
Image binding from public handoff: `synthetic-qa-zns-fqa-app@sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`.
Stand: `synthetic-qa-zns-fqa-recovery`, UI `http://127.0.0.1:58413/`.
Method: fresh headless Edge through Playwright, actual UI actions and screenshots, supplemental public response observations. No source, diffs, engineering reports, or other reviewer evidence inspected. No rebuild, reseed, restart, database mutation, or product edit.

Verdict: supported manual UI subset passes as listed below. Original R01 remains PARTIAL; no full D/E acceptance. Lifecycle, database faults, durable queue/retry, resource topology and importer requirements remain BLOCKED by absent handoff capabilities.

## Observed coverage

Evidence files are in `qa.local/fqa-recovery-import-reviewer/`.

| Check | Result | Evidence |
| --- | --- | --- |
| Message input and locale selection | PASS. `/language`, `en`, `/orders`, `/start` and later `ru` produced application locale controls and visible history. Wait for asynchronous processing before judging final text. | `language.png`, `en-start.png`, `en-selected.txt`, `ru-orders.txt` |
| EN manual select/confirm/cancel | PASS. Transfer selection created draft version 2; confirm booked version 3; cancel cancelled version 4. Existing bot card message 2 changed in place. | `en-transfer-selected.png`, `en-confirmed.png`, `en-cancelled.png`, `ui-network.json` |
| RU manual select/confirm/cancel | PASS. Transfer selection draft version 5; confirm booked version 6; cancel cancelled version 7. Russian status and controls shown. | `ru-selected.png`, `ru-confirmed.png`, `ru-cancelled.png`, `ui-network.json` |
| Callback admission and effects | PASS for callback-shaped events admitted with update IDs 14–16 and 18/20–22; corresponding state visible. Separate Telegram callback-answer acknowledgement UNAVAILABLE: public state response has no acknowledgement ledger and UI exposes no completion toast/answer record. | `ui-network.json`, workflow screenshots |
| Prior message edits | PASS for ordinary workflow and locale edits. Message 2 persists across versions; service/action controls change with state. | `en-start-network.json`, `ui-network.json`, workflow screenshots |
| Exact provider hold/release edit | PASS for one documented `before_apply` case. Alice101 message2 stays Cancelled version7 while `held_before_apply` is observed; release acknowledgement `released_unresolved` then terminal `completed` with known success. Browser UI and public card show exact expected Draft version8 text afterward. No crash/restart/uncertain-send claim. | `control-held.png`, `control-held.json`, `control-completed.png`, `control-terminal.json`, `control-completed-ui-state.json` |
| Document upload/download | PASS. Browser file picker upload `reviewer-proof.txt`, 47 bytes; actual visible file link clicked and browser download saved. Download equals `FQA recovery immutable upload proof 2026-10-01` plus newline. | `ru-upload.png`, `download-proof.txt`, `ui-flow.cjs` |
| Synthetic user isolation | PASS for Alice101→Visitor303. Visitor initial conversation/history is empty; Alice document and workflow absent. Visitor `/start` renders own service controls. Role labels are not permission proof. | `visitor-isolation.png`, `visitor-start.png`, `isolation.cjs` |
| Optional documented model fixture | CAPABILITY CONFIRMED. With EN selected through UI, fixture installs update 28; public model state accepted 1/rejected 0; actual UI opens profile with full-name and role buttons. This proves deterministic proposal routing only. | `fixture-install.json`, `fixture-state.json`, `fixture-ui.png`, `fixture.cjs` |

Service display names remain Russian in the EN workflow fixture. Existing fallback text in prior attachment cards also remains Russian when controls change to EN. This report does not infer a new-response localization defect from an existing historical fallback string or a fixture service name.

## Original scenario disposition

- R01: PARTIAL. Supported messages, inline buttons, ordinary edits, document upload/download and deterministic fixture route observed. Reply-keyboard behavior, every media kind, explicit callback answer, general agent behavior and real integrations are not proved.
- R02–R07: BLOCKED. No receipt/commit/delivery crash-boundary or lifecycle/session/database handoff; no authorized replacement/restart window.
- R08–R14: BLOCKED. No full durable retry, invalid-delay, bot/lane pacing, announcement ordering, recipient/credential failure, or permission-before-deferred-send control contracts. Ordinary completed manual workflow cannot prove these.
- R15–R16: BLOCKED. No registration-announcement initiation-rank/expiry fixtures and clock control. Booking draft versions are not evidence for registration announcement intent order.
- R17: BLOCKED. No agreed collector-failure control.
- R18: BLOCKED. Public handoff states six managed isolated components but gives no public resource/process measurements needed to prove the specified CPU-only main app/PG/helper topology.
- I01–I09: BLOCKED. Import has no live handoff and is unprepared. No importer actions attempted.

## Control readiness

Public control at port 58414 initially reported `idle` with no events. Port 58413 `/control/state` returns 405; the separate documented control URL is required. Lead supplied exact arm/release contracts and approved the isolated before-apply case. Armed chat101/message2 and SHA256 of predicted EN Draft version8 replacement text, derived from this reviewer's observed prior Draft version2 text. Actual UI transfer click reached `held_before_apply`. UI retained version7, then one body-free release completed the edit. Terminal control state is `completed`; no held request remains. This proves only the exact ordinary edit hold/release path.

## Harness limitations

One first browser script assumed English service name `Transfer`; locator timed out against the synthetic Russian name. A later script used the observed UI label and completed both workflows. An isolation attempt used nonexistent user 103; selection timed out without mutation, then UI option inspection identified Visitor303 and isolation completed. These are harness errors, not product failures.

Evidence captures bounded synthetic history only. No claim is made about physical devices, real Telegram acknowledgement timing, real providers, Zitadel, natural reasoning, restart persistence, or actual Python production-source preservation.
