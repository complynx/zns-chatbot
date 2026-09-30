# Inbox Functional QA 02 — 2026-09-30

Status: BLOCKED — required functional acceptance is not established.

## Scope and independence

Fresh, source-blind Functional Senior QA. Read only `qa.local/aud10-stand-20260930/functional-access.md` for original requirements and stand access. No product source, prior QA reports, developer findings, progress/audit records, tests, lint, Docker mutations, or SQL inspected/executed. This report is owned by reviewer 02.

Frozen runtime digest supplied by the stand access document: `synthetic-qa-zns-aud23-app@sha256:b1ac3204d630ddb723a09507d20b933b6b737338335e04bbf1c6693833253aa7`.

## Actual observations

- Initial CUA inventory exposed one Codex In-app Browser (ID 1), no native apps.
- Created a hidden tab (ID 1) at `http://127.0.0.1:58118/`; actual Telegram-like UI loaded. Alice was selected. Existing history showed English language acknowledgement, English profile/pass/booking controls, Russian historical notifications, synthetic user selector, message form, callback buttons, and file upload form. These are observations of pre-existing state, not successful scenarios run by this reviewer.
- Created synthetic fixture `qa.local/aud10-functional02-20260930/retry-proof.txt` containing only an artificial QA marker.
- Read the CUA file-uploads documentation before attempting selection.
- Attempted the documented file chooser flow: filechooser wait timeout 10,000 ms; upload-control click timeout 10,000 ms; setFiles timeout 10,000 ms; tool timeout 25,000 ms. The tool did not return within its requested bounds. It was interrupted after 385.3 seconds. No successful selection or send action was observed. The Send file button was never clicked by this reviewer.
- After interruption, a single read-only `tab.getAXState()` returned `Browser is not available: 1` in approximately 3.6 seconds. A fresh CUA inventory returned exactly `{"apps":[],"browsers":[]}`. No further chooser attempt was made.
- Root reported fault controls remained OFF. No controlled fault or runtime restart was requested before the interruption.

## Acceptance coverage

Not exercised: per-chat FIFO; independent-chat progress during a poison update; bounded durable 5s/30s/2m/10m retries; fifth-failure quarantine; retained payload/safe diagnostic; quarantine barrier release/no automatic replay; no duplicated business effects or media delivery; cooldown persistence across restart; parent-cancellation budget; SQL-failure stop/recovery; Telegram retry_after pacing and restart; new EN/RU messages, callbacks, permissions and upload flows.

No product defect is established by this run. The chooser hang and lost browser inventory are tool/infrastructure blockers, not evidence of a product failure. Existing visible UI is insufficient to pass the required gate.

## Recovery and limitations

Requested a minimal loopback-only auxiliary browser fixture button for selecting/submitting the fixed synthetic upload through the existing stand contract, while retaining the original Telegram-like UI for messages/callbacks/history. It had not been provided or exercised at this report checkpoint. File-picker coverage would remain separate from upload/inbox behavior even if that facility is supplied.

Firefox connector tool metadata was discoverable, but there was no granted stand tab available to this reviewer. No unrelated Firefox tab was navigated and no page content was read through that connector.

Synthetic model credentials are unused. Real-model interpretation, ASR, real Telegram/provider behavior and physical interaction are not accepted. HTTP-only checks were not substituted for required UI acceptance.
