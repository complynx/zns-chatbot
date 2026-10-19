# Native avatar generation and delivery status

Source schema57 / manifest mvp65 now exposes Generate and send in the existing
RU/EN avatar form. This UI adds no schema, dependency or production setting.
The installed primary remains schema49 / mvp56; this source is not installed there.
The separate [owned runtime gate](shuttle-avatar-runtime-qa-2026-10-19.md) now passes.

## User flow

Upload or select an original, edit its crop, save, and preview the saved version.
Generation requires that preview and a refreshed server document list. It creates
one immutable JPEG and a delivery intent through the common
[finalization API](shuttle-avatar-finalization-api.md), with no Loom run required.
The form does not offer another generation for a draft/version already present
in the server list. Save a changed draft version to make a different document.

Created documents lists retained saved-draft artifacts. Refresh delivery status
reads current state independently of the editor. Queued, sending, delivered,
failed, blocked, dismissed and unknown are separate states. A persisted JPEG is
not proof of Telegram delivery. Unknown delivery asks the person to inspect the
chat and request administrative resolution if a retry is needed; it never
silently creates another document or resends one. The form does not expose
administrator retry/dismiss controls.

Download JPEG reads owner-authenticated bytes, checks their size and SHA256
against the receipt, and starts a browser attachment download. Download does not
change Telegram delivery. Changing the selected document, refreshing, or logging
out releases the prior download URL. No image bytes enter browser recovery storage
or agent conversation JSON. Generation does not change the Telegram profile photo.

## Recovery and handoff

Before generation, sessionStorage retains the owner, operation UUID, draft ID
and expected version. A lost reply keeps this exact request pending. Check earlier
request reads its immutable finalization receipt; Retry uses the same ID and
payload. A recovered receipt and current draft are separate: later agent edits
remain visible without changing the generated document. Missing browser metadata
can be recovered from the owner's bounded server document list.

Storage failure prevents writes. Epoch guards fence both late success and errors
from a previous session/owner. Draft create/save/upload keep their existing
recovery contract. Current UI state and an old receipt do not imply new consent.
The shared original/artifact evaluation quotas and undecided production retention
policy remain unchanged.

## Acceptance boundary

All17 avatar UI tests and all207 Mini App behavior tests pass. Owned headless Edge
passes14 real DOM/canvas checks with a mock HTTP backend, including an actual JPEG
download event, filename and byte hash under the page CSP. The RU mobile and EN
screenshots were inspected. The browser and fixture HTTP server are closed.
That fixture is distinct from the subsequent [real IAM/Loom runtime gate](shuttle-avatar-runtime-qa-2026-10-19.md),
which passes native-only with Loom stopped, agent-only and both handoffs, including
a normal bot restart. Actual Telegram WebView and unattended model acceptance
remain open; administrative repair UI and production retention are separate work.

Evidence is `.cache/shuttle-work/avatar-send-ui-v1-qa/`; failed initial test fixture
assertions are retained separately from passing results. Independent correctness
and quality reviews found no actionable changes.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
