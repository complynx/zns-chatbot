# Case29 independent Functional QA

Original admitted fixture-backed F03: ACCEPTED in all four required cells. Current physical closure is supported by the final neutral release and retained actual receipts, with the raw outer-tool transcription limitation below. Full Stage C, real-provider final acceptance and migration final acceptance remain UNPASSED. Missing evidence is not PASS.

Reviewer: /root/c_fqa_case29_current, gpt-6.1-sol/Codex. Protocol identity: c_functional_db71597; compatibility identity: functional_e_reset_current. Review was implementation-blind. I read original F03, neutral access/contracts, current admission/runtime receipts, and the current reviewer's saved DOM, screenshots and action outputs. I did not read product/helper implementation, engineering findings or historical QA outcomes. I issued no duplicate browser execution, ACK or business input.

## Current binding and admission

- Component source: db7159723cc194d71387e0525986e907e269ec56.
- Product image: sha256:ffbb28f12b813a98ba41292987cac9130588c7f1f7fa8e88e40dc7eb24340ed0.
- Owner: 261addf72e3041a1a566f2f3a3a86214cef8352b1898b757973a8ceabd9a3ba9.
- Browser: a513b6cb0d5d74a99ec312f7b893b61d0542778236e00e59ee56b7d3bf88f270.
- Created SHA256: c29e709e30615deb8492fd78bd0337d2ce193751a1d6130c0a4705a3172d4790.
- Conditional grant SHA256: c0bb568969354dbaeb63be9305d9288916141e8a41b916fe8fd67c325f790535.
- Runtime admission SHA256: 80e5e6145c55b1d9723c7d982e76849d7d7d9e086a18462bc15944520b14a112.
- Operator: PID95480, birth2026-10-10T02:59:02.3557073Z, session22753.
- Action/caller/input hashes were independently checked against Created. Immutable mounts and sole reviewer RW mount were declared in the actual Created record.

READY at03:30:44.5598823Z preceded the first valid observer sample at03:30:46.7296473Z and business ACK at03:30:52.313Z. ACK binds the exact owner/browser/admission, READY and binding hashes. First sample contains ten unique current IDs, all running, healthy for declared healthchecks and explicit none otherwise. The actual read-only admission observation uses zns_inventory, transaction_read_only=on, and backend4139/birth03:30:39.333155Z with both current admission keys granted for installation010800000220/launch5419867b1c7d43a5482a0ea3. All69 sample records contain ten profiles and live admission observations. Intervals between samples remain unobserved.

## Actual displayed behavior

| Cell | Initial stored UI / question / answer | Changed stored UI / reverse question / answer | Old-card ACK | Fresh UI persistence |
| --- | --- | --- | --- | --- |
| EN/mouse | EN / RU / RU | RU / EN / EN | callback6, message4, status200/resulttrue | RU, cursor10, edits4 |
| EN/emulated-touch | EN / RU / RU | RU / EN / EN | callback13, message4, status200/resulttrue | RU, cursor16, edits13 |
| RU/mouse | RU / EN / EN | EN / RU / RU | callback19, message4, status200/resulttrue | EN, cursor22, edits19 |
| RU/emulated-touch | RU / EN / EN | EN / RU / RU | callback25, message4, status200/resulttrue | EN, cursor28, edits28 |

Each cell has actual locale, both reply, old-control, navigation and fresh-session DOM/screenshot captures. I visually inspected reply screenshots across all four cells, all four old-control screenshots and all four fresh screenshots, and assessed both reply directions from the actual saved DOM body and displayed controls. The displayed profile headings, field prompts and four controls use stored UI locale. The appended fixture answer uses the current question language independently. For example, English Your profile/Set full name controls coexist with the Russian answer Ваш профиль открыт; Russian Ваш профиль/Указать полное имя controls coexist with Your profile is open. These are separate language obligations, not stale mixed profile labels.

After locale change, old message4 was refreshed before its displayed control was invoked. The callback receipt, processed cursor and edit increments correlate with the observed current card. Post-callback cards contain the changed-locale profile prompt and controls with no prior-language profile labels. Required original F03 permits invoking the old card OR navigation/back; the old-card branch is actually covered. Visible /start navigation is also captured.

Fresh independent document sessions have actual GET / responses200, document commit/load observations and actor/persisted-card visibility. The changed locale, profile controls, current reply, history and edit count persist. Fresh visibility occurred at03:31:13.844Z,03:31:39.356Z,03:32:02.430Z and03:32:26.724Z, within their original10s operation deadlines. This is UI/session persistence evidence, not application replacement/restart evidence.

The English service card displays English status and places labels but Russian service titles such as Массаж · 20 минут and Трансфер · до площадки. The supplied neutral contract does not say whether those titles are translatable catalog labels or stored business content. I do not classify them as a proven stale-label defect or claim full English service-title localization. The accepted scope is the tested profile/card language behavior.

## Clocks and completion

Original limits retained: F30/G600/START400/readiness60/cleanup120/native15/model10, normal shutdown target1s and hard5s. No clock was renewed.

Binding separately records Linux M0=03:30:13.0282059Z and host M0=03:30:14.4203454Z. Actual raw Docker START=03:30:18.796298178Z,5.768092278s after Linux M0. Host before/observed=03:30:19.6949894Z/03:30:20.7711833Z, both within host F30. UTC elapsed1.0761939s versus ticks elapsed1.0761925s differs by0.0000014s, below the original100ms continuity bound.

The public exact equations recompute active03:36:58.7962981Z, readiness03:31:18.7962981Z, cleanup03:38:58.7962981Z and browser03:38:53.0282059Z. LinuxOverall=03:40:05.6693070Z and HostOverall=03:40:07.0614465Z remain distinct. Tick carriers agree: active5462290854815/readiness5458890854815/cleanup5463490854815/overall5464173506265. READY, binding, prearm and runtime-window correspondence was checked. No clock.json exists; I used the documented READY.clock_window witness rather than inventing a clock receipt.

Inputs ceased03:32:28.430Z. Own done03:32:28.528Z records inputs_ceased=true, qa_operations_pending=false and accepted_input_completion_uncertain=false. Actual consumer terminal exit0 at03:32:28.7730955Z. I independently read these before writing release.json. Release SHA2564ddabd4bb870c1c5774d83380a77410e35eb635c681a62f20a5a713b5e69b0cd. No further business actions are permitted by this release.

Observer terminal:69 attempts/69 valid samples,138native captures, max sample gap4.2229621s, no retained native fault, no raced/unproved boundary, native custody physically released. This does not establish continuous health between samples.

## Physical closure reviewed

- Business child/supervisor physically absent; wait/reap and sealed stdout/stderr recorded; join619.5706ms.
- Browser SIGTERM shutdown161ms; Chromium child exited0, close and both EOFs observed, kernel_reaped=true.
- Managed cohort records normal TERM; normal stop upper2.51s satisfies hard5 but does not demonstrate aggregate target1.
- Cleanup entered03:35:11.9058584Z and ended03:35:31.1108607Z, about19.205s within original120s. Cleanup native and watcher receipts record kernel reap, both EOFs and physical custody released, exit0/no fault.
- Current container/network/volume absence output files are empty; namespace receipt names the exact Case29 namespace.
- Observer-host-terminal.json still explicitly records kernel_reaped=false. Native capture release does not prove host-observer kernel reap.
- outer-exit-request.json records ordinary exit request03:35:31.1595389Z. An exit request is not an independently observed actual outer terminal.

The subsequent neutral PHYSICAL-RELEASE.json closes the earlier missing physical conditions without changing the early observer receipt. It binds the same admission/owner/browser and retained receipt hashes; records371native receipts with no bad closure entries; records the final observer join via the outer physical-release witness, current observer PID absence, and unchanged early kernel_reaped=false snapshot. Actual outer synchronous return exit0 is recorded at03:35:31.2189359Z, followed by exact operator PID absence at03:35:52.6425101Z, within the original bounds. Browser, ten container IDs, namespace networks/volumes and native physical custody are closed. The raw outer tool chunks75a285/1018d9 are transcribed by the operator into this neutral receipt; I did not independently consume those original tool outputs. That provenance limitation is retained, not converted into a fabricated native receipt. Final observer absence was observed03:39:48.0369115Z; this later observation corroborates physical absence, not a renewed cleanup clock or a claim that shutdown occurred then.

## Remaining acceptance boundaries

No semantic profile-language failure was demonstrated in this run. The original F03 row, docs/qa/fqa-flows-20260930-plan.md:33, requires locale/question independence, old-card navigation/back OR callback, refreshed stored-locale cards/buttons, new-session persistence and fixture limits. All are covered in the current admitted fixture scope. It does not itself require an application replacement/restart, cross-user rights scenario or financial-effect scenario.

The broader neutral intake CURRENT-NEUTRAL-INTAKE-REQUIREMENTS.md asks to keep rights/effects/restart observations separately unpassed. Those gaps are not promoted to missing original F03 capabilities. Their original plan mapping is F05–F06 authorization/privacy, F08 failure/interruption and duplicate mutation, and F13 outage persistence (docs/qa/fqa-flows-20260930-plan.md:35-38,43). Stage C requires the broader F01–F08 slice at line47; those scenarios are not accepted by this F03 run. Real-provider natural understanding and actual language switching remain separate mandatory final acceptance at line25. The same line limits fixture passes to orchestration/policy behavior. Physical touch is disclosed unverified in the neutral intake and ACCESS; emulated touch does not establish physical-touch device behavior. Migration/full-final acceptance is separately excluded at line47.

Synthetic actor101/profile/callback evidence proves the exercised authorized fixture UI behavior only; it does not prove cross-user isolation, revoked authority, retries or duplicate financial/effect protection. No skipped scenario is recorded as passed. The original fixture F03 acceptance and the wider unpassed gates remain distinct.

Written by c_fqa_case29_current (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
