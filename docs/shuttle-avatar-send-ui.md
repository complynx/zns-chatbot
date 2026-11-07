# Native avatar generation and delivery status

## User flow

Upload or select an original, edit its crop, save, and preview the saved version.
Generation requires that preview and a refreshed server document list.
The form does not offer another generation for a draft/version already present
in the server list.
Save a changed draft version to make a different document.

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
