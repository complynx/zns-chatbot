# Functional QA inbox — 2026-09-30 — independent run 03

Status: PASS for the exercised synthetic durable-inbox acceptance scope. No product defects found. Native picker, live Telegram and real model/provider behavior are outside this proof.

## Scope and isolation

Fresh source-blind Functional Senior QA. Read only the original requirements and stand contract `qa.local/aud10-stand-20260930/functional-access.md`; no product source, earlier reports, audit, progress or developer findings. Report ownership overridden to this file and `qa.local/aud10-functional03-20260930/`.

Frozen app digest: `synthetic-qa-zns-aud23-app@sha256:b1ac3204d630ddb723a09507d20b933b6b737338335e04bbf1c6693833253aa7` (provided stand contract, not independently verified).

Browser: fresh hidden Codex IAB tab at http://127.0.0.1:58118/. Root owns fault control, restarts and SQL observations. Synthetic users/data only.

## Chronological observations

- 08:23–08:25 UTC: browser UI available. Selected Guest via user selector. Clicked massage callback in EN; browser action history recorded update 23, `forbidden`, while order remained Not selected, version 0. Clicked ru language callback; visible language reply changed to `Язык сохранён: ru. Текущий язык: ru.`, update 24. Clicked /orders; current order card rendered RU with unchanged version 0. UI history updates asynchronously; immediate click snapshot can precede processing.

## Limitations

- Synthetic model credentials unused. No real-agent intent interpretation, ASR or real-provider acceptance.
- Native file chooser unavailable; do not claim native picker coverage. The auxiliary browser form submitted fixed synthetic media through the published sandbox upload contract.
- Durable retry, quarantine, restart, pacing and no-duplicate-effect evidence completed below.

## Natural retry scenario started

- 08:30:30.316098 UTC: submitted Alice101 fixedTXT exactly once through auxiliary browser form at 58120. Visible receipt HTTP200, update28, file `ROTLRNB7LJZNLCD2PF3AE23XCV`, caption `functional03 permanent failure`. Root armed five getFile503 responses for that file immediately before submission.
- Original Telegram-like UI displays one document input and caption. Queued `/language ru`, then `/orders` in Alice chat. Both inputs visible, no replies yet; EN state remains. Switched to Guest, clicked EN language callback (update31); visible reply and order card changed to EN while Alice failed file was pending. Guest /orders intake accepted.
- Root-supplied metadata at 08:30:49Z (not reviewer SQL access): requests at 08:30:30.412Z and 08:30:35.503Z; update28 pending, ordinary failures2, next08:31:05.503647Z; same-chat update29 pending failures0. Preserved payload MD5 `548b5f57bcd50ee7f55c139f5cc63ef7`.
- Observed prerequisite PASS: browser upload intake, same-chat barrier (so far), another chat progresses, EN/RU manual commands and permission callbacks. Full retry/quarantine verdict remains pending.
- 08:32 UTC root snapshot: update28 pending failures3, next08:33:05.576043Z, unchanged payload hash. Updates29/30 samechat101 pending failures0. Guest31 no longer retained as pending.
- 08:33:05.650 UTC root observed fourth503; next08:43:05.651029Z, failures4, same payload hash; updates29/30 remain blocked with failures0.
- 08:33:57.923380 UTC submitted Guest303 fixedPNG once from browser helper. Receipt HTTP200, update33, file `QWVBE2GDOFWIVMXCZG6TIZNLS6`, caption `functional03 independent photo`. Original UI shows one photo input then `Attachment notice / I cannot access or process this attachment. Please send it again.` Successful media processing is NOT established. The Alice-only fault does not intentionally target this file. Root asked for safe runtime observations and restart after this completed visible response.
- Root controlled restart completed by 08:35UTC. Root metadata: all six runtime containers replaced, app healthy, update28 still failures4 and exact due08:43:05.651029Z with unchanged payload;29/30 pending0. Alice getFile count still4, no extra requests after restart. Guest33 getFile returned noninjected200; root independently decoded supplied PNG valid2x2RGB, core media rows0. UI attachment-notice flow completed, but successful media remains unproven.
- 08:36UTC Boris202 `/qa_cancel_once_20260930_02` submitted once. UI returned normal RU service prompt. Root found cancellation fixture did not match internal test identity; no active operation observed and no shutdown performed. Update35 is NOT cancellation proof. Root correcting stand fixture and will confirm before second attempt.
- Controlled in-flight cancellation: corrected command update37 was active at 08:38:38.875985Z (`Timeout/PgSleep`, started08:38:38.171881), per root observation. Root stopped runtime owner; durable update37/chat202 pending0, next-infinity, payload MD5 `4d3f1e7f113bf4035bae15ebb87a5ddf` unchanged, managed runtime fully stopped. Root restarted unchanged owner. By08:39UTC row37 completed; browser action history independently showed attempt2 and exactly one input/reply/reply_origin for37, with one unchanged RU service card. No reviewer resend after cancellation. PASS scoped to genuine in-flight cancellation, unchanged ordinary budget, payload survival and automatic processing after supervised recovery. Evidence supplied by root: `qa.local/aud10-stand-20260930/fqa03-cancel-active.txt`, `fqa03-cancel-before-stop.txt`, `fqa03-cancel-stopped.txt`, `fqa03-cancel-stopped-owner.txt`, `fqa03-cancel-replayed.txt`, `fqa03-cancel-replayed-runtime.txt`. Alice failure4 and exact08:43:05.651029Z deadline remained unchanged throughout.
- SQL failure case update38: submitted Boris marker once. Root app capture: request08:41:51.736Z to fatal error/stopped08:41:51.781Z. Repeated root snapshots08:41:52–08:42:03 captured pending failures0 next-infinity and unchanged payload hash `6d953e46dc68188c8a3124df047f8ca8`. Supervisor automatically restarted (RestartCount0→1; root did not request restart), then processed update38. Browser independently confirmed attempt2 and exactly one input/reply/reply_origin, unchanged single RU service card. PASS scoped SQL-failure shutdown, budget preservation, supervised automatic recovery and no duplicate reply. Root evidence: `qa.local/aud10-stand-20260930/fqa03-sql-observer.txt`, `fqa03-sql-app-live.log`, `fqa03-sql-after.txt`.
- Fifth503 at08:43:05.978Z. Root metadata: update28 quarantined with failures5 at08:43:05.980162Z, original payload hash unchanged; updates29/30 completed, each language/orders reply exactly1. Browser independently confirms Alice RU acknowledgement and RU orders card after the barrier released. PASS natural 5s/30s/2m/10m retries, fifth-failure quarantine and same-chat FIFO release, corroborated root request timestamps; no shortened deadlines. Root evidence `fqa03-503-final-state.json`, `fqa03-inbox-quarantine.txt` under stand evidence directory.
- 08:44:01.718913Z: Boris202 TXT pacing probe submitted once via browser helper, update40, file `3CSUXRD36OOEDKQWUUCO7AS7RJ`, caption `functional03 pacing retry_after60`. Root armed single429 retry_after60 before submission. Original UI shows one document input and no bot response during pause. Root requested to restart during60s, then observe quarantine28 beyond its retained next-attempt metadata08:53:05.979027Z.
- Pacing PASS: root observed429 at08:44:07.177104Z; pending40 failures0, due08:45:07.179111Z, payload hash `c3c06d5cfbcf317d4e898dbc2aea1558`. Restart08:44:28–08:44:34;08:44:53 snapshot kept exact deadline/hash/budget and showed no early retry. First noninjected200 at08:45:07.381531Z, 60.204s after429. Browser then showed exactly one `Вложение tg-media-40` card with manual receipt/avatar/other/cancel buttons. Agent-unavailable text correctly limits synthetic-model claims. Root core media1 row for Bob, hash `3e358ba36dc76483e87d06c9a78d3869`; update40 completed, interactions one each. Root evidence `fqa03-429-{before,after}-restart.json`, `fqa03-429-inbox-{before,after}-restart.txt`, `fqa03-429-after-due.json`, `fqa03-429-replayed.txt` in stand evidence directory.
- 08:46–08:47UTC: Boris selected transfer once (draftv1), confirmed once. UI card became `Забронировано · версия 2`, transfer20BYN. Pending attachment card did not trap the unrelated manual booking callback. One booking card and one media card visible. Root requested to baseline committed business/media effects before final controlled restart.

- 08:48UTC final controlled restart after committed booking: browser page reloaded, reselected Boris. Exactly one booked transfer card version2 and one tg-media-40 card remain. Alice RU language/orders also persist. Root pre-restart baseline: select update42, confirm43; bob/shuttle-1 bookedv2; audit selectv1count1/confirmv2count1; core operations2, media1 with unchanged hash. PASS current UI persistence; final post-restart/count comparison supplied separately by root. Root evidence `fqa03-business-before-restart.txt`, `fqa03-business-after-restart.txt`, `fqa03-business-runtime-after-restart.txt`.
- Post-restart root comparison confirmed zero differences in workflowv2, audit counts, operations2, media1/hash, interactions40/42/43 and quarantine28. Evidence `qa.local/aud10-stand-20260930/fqa03-business-restart-diff.json`. PASS no duplicate committed business effects or successful media processing/delivery across the exercised retries and restarts.

## Current acceptance matrix

- PASS: actual Telegram-like UI messages and callbacks in EN/RU; forbidden guest booking; independent-chat progress; same-chat pending FIFO and release.
- PASS: ordinary durable retries at natural5s/30s/2m/10m intervals, fifth-failure quarantine, payload preservation, safe diagnostic metadata, preserved pending deadline across restart.
- PASS: genuine in-flight cancellation preserves ordinary budget/payload and reprocesses automatically after supervised recovery.
- PASS: SQL failure stops runtime, preserves ordinary budget/payload and recovers through supervisor.
- PASS: structured Telegram429 honors60s across restart and does not consume ordinary failure budget; successful authorized media is processed once.
- PASS: booked version2 and one media record persist; no duplicate committed business/audit effects after restart, corroborated by root metadata and browser UI.
- PASS: no automatic replay after quarantine retained next-attempt timestamp08:53:05.979027Z. Root observed at08:53:07.616816Z that28 remained quarantined5 with the original payload hash and exactlyfive poison-file requests, last08:43:05.978Z. Final browser UI showed no new or duplicate Alice message. Quarantine untouched and faults disarmed.

Limit scope: browser upload helper bypasses only native picker and submits fixed synthetic TXT/PNG through the same stand upload contract. No native picker, live Telegram, real model/ASR/provider, arbitrary media-format, or production acceptance. Root owns and supplies SQL/runtime observations; reviewer independently performed and observed browser flows. No product source inspected, no product edits, no rebuilds by reviewer. The failed Guest PNG is an observed denial/fallback, not successful-media proof; success is established separately by allowed Boris TXT40.


## Final verdict

PASS for this synthetic inbox stage scope. Actual browser messages, callbacks and fixed-file uploads corroborate runtime metadata. Natural retry/quarantine timing, cross-chat progress, FIFO release, restart durability, active cancellation, SQL-failure shutdown/recovery, Telegram pacing, and absence of duplicate committed business/media effects passed in the exercised scenarios. No substantive product defect found; no dependent requirement remains incomplete within this synthetic scope.

At08:53:07.616816Z root final metadata showed quarantined28 still at failures5 with unchanged payload, no extra poison request after08:43:05.978Z, booked Bobv2/auditselect1/confirm1/media1 unchanged, proxyOFF and no QA SQL triggers remaining. Final browser observation after that snapshot showed the same single Alice document input, RU language acknowledgement and RU orders card; no automatic replay or new attachment result. Evidence: `qa.local/aud10-stand-20260930/fqa03-final-proxy.json` and `fqa03-final-state.txt`.

Chronological pending statements above describe intermediate observations and are resolved by the final matrix/verdict. Skipped limitations remain skipped, not passed. Root performed all infrastructure mutations and supplied metadata; this reviewer made only its owned report edits and synthetic browser actions.

Written by Codex (model slug not exposed/Codex desktop)
on behalf of Daniel Drizhuk
