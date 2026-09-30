# Functional QA inbox 2026-09-30 01

Status: INCOMPLETE — browser facility unavailable; no acceptance inferred.

## Scope and independence

Fresh source-blind Functional QA. Read only `qa.local/aud10-stand-20260930/functional-access.md` before test design. No product implementation, prior reports, audit, PROGRESS or developer logs inspected. Infrastructure mutations and SQL observations remain owned by coordinator.

Stand: http://127.0.0.1:58118/. Supplied frozen digest: `synthetic-qa-zns-aud23-app@sha256:b1ac3204d630ddb723a09507d20b933b6b737338335e04bbf1c6693833253aa7`.

## Observed browser evidence

Actual Telegram-like browser UI opened in hidden IAB, tab 1. Visible:true is unsupported from the subagent thread. UI displayed synthetic Alice, Boris administrator, Guest view-only, channel and forum selections; message history, callbacks, text sending, contacts and media upload controls were visible. Alice's pre-existing current English view included booking Draft version 1, Confirm and Cancel booking buttons, assigned pass, registration profile, and Language saved: en. Pre-existing historical Russian notifications were also visible. These are observations of existing state, not passes for newly exercised behavior.

Independent scenario selected: ordinary HTTP503 for one synthetic Alice upload through all five real attempts; same-chat queued RU then EN changes; Guest independent progress and permission flow; controlled restart during pending cooldown; quarantine barrier release, retained payload and no automatic replay; subsequent structured 429 pacing and restart. All require successful admission before dependent assertions.

Coordinator reported HTTP503 count5 armed at 2026-09-30T07:57:10Z, first-file binding pending. QA created only `qa.local/aud10-functional-20260930/poison.txt`, artificial text payload. Upload tool invocation attempted documented chooser flow: wait for filechooser, click file label, set fixture path, fill caption. It returned no intermediate results and hung for 592.5 seconds before coordinator interruption. It did not include a Send file click. Exact partial execution cannot be established.

After interruption, read-only `tab.getAXState()` returned `Browser is not available: 1`. Read-only inventory returned no browsers or apps. An authorized fresh hidden IAB tab creation returned `Browser is not available: iab`. No process restart, blind upload repeat, or infrastructure change was performed by QA. Supported browser documentation expressly exposes filechooser.setFiles and advises against locator.setInputFiles; no direct upload alternative was documented.

Coordinator independently reported zero getFile requests and first-file unbound while awaiting upload. This is not evidence of product failure or success.

## Outstanding acceptance

Not exercised: new EN/RU manual flows, permissions, current-message updates, per-chat FIFO, independent chat progress, ordinary retry intervals and budget, quarantine and no autoreplay, persistence across restart, duplicate business/media effects, parent cancellation budget, SQL failure fail-stop/recovery, Telegram retry_after and restart pacing. Required UI mutation coverage remains incomplete.

Synthetic model credentials are unused. Real model interpretation, ASR and real Telegram/provider acceptance cannot be claimed by this stand.

## Facility blocker

Browser backend unavailable following the interrupted filechooser invocation. Coordinator informed immediately after returned errors. Recover browser facility before further UI acceptance; an endpoint upload helper, if used, must remain explicitly separate from browser upload coverage.

## Recovery attempts

Coordinator restored a parent-host visible IAB tab 4 and supplied browser ID 1. Subagent attachment to tab 4 using both `browser: iab` and exact `browser: 1` failed immediately with Browser is not available. Resetting only the subagent CUA JS kernel and reattaching did not restore access. No parent tab was closed; no browser process or stand was restarted by QA. Parent-host UI availability is coordinator-reported, not independently observed here. The facility limitation persists specifically on this subagent surface. Initial incomplete verdict remains unchanged.
