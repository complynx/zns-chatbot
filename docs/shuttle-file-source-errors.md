# File source failure classification

The file API distinguishes an explicit source refusal from a temporary read
failure without changing the caller's authorization or persisting a lost-file
flag. Every retry still requests a fresh Telegram file path.

| Observed source result | API response |
| --- | --- |
| Coherent getFile400 with exact `Bad Request: invalid file_id` |404 `file_unavailable` |
| Coherent getFile403 or download403 |424 `file_source_access_denied` |
| Ambiguous getFile400, unknown/malformed/incoherent response,429/5xx, transport failure, or download404 |503 |
| Known size excess or explicit supported Telegram size refusal |413, unchanged |

Source424 is deliberately different from caller401/403: refusal by Telegram
does not mean that the user lost permission to use the application. Existing
local authentication, revocation and hidden foreign-file404 remain unchanged.
Ownership is checked before source access and again before successful byte
delivery. The raw content endpoint returns the same status distinction with
generic HTTP text; upstream bodies and credentials are never returned.

Telegram's [getFile contract](https://core.telegram.org/bots/api#getfile)
provides renewable download links. Its [current server implementation](https://github.com/tdlib/telegram-bot-api/blob/master/telegram-bot-api/Client.cpp)
also has an explicitly ambiguous invalid-ID-or-temporary-download failure.
An arbitrary400 or a download-path404 is therefore not evidence of permanent
loss. Unknown descriptions keep the conservative503 fallback. Even an explicit
invalid-ID result describes this request; no permanent deletion or source tombstone
is inferred. File metadata and submitted evidence remain available under their
existing access rules, and a later successful read remains possible.

## Verification

The focused regression first failed with503 for an explicit invalid ID and both
source-refusal paths. It now passes12 API cases, including coherent failures
inside HTTP200, near-match descriptions, unauthorized service credentials,
malformed403, incoherent codes, rate limiting and temporary download404. Each
case subsequently succeeds through a fresh getFile request without changing
the saved file record.

The PostgreSQL/HTTP integration test checks opaque-ID fetch and raw content,
foreign-file denial before any Telegram IO, unchanged owner metadata/authority,
and invalid-ID → source refusal → temporary failure → successful read. Existing
intake/restart/size/privacy scenarios are reused through the focused file-test
selection. This uses synthetic Telegram/IAM peers and an isolated test database;
no paid model, real Telegram, production data or external ASR was used.

Final affected strict lint reports zero issues, and the eight-file source/test/doc
review is clean. Source `035f2a6b` built local Linux/amd64 image `bf1383e8` with
UID10001 from a tracked-only archive. The isolated synthetic test database was
archived, fully decoded and removed. No image publication or installation occurred.

This accepts the API/transport classification. It does not establish autonomous
model recovery wording or real source deletion. Existing bounded model voice
and local-pilot receipts retain their exact image and scenario scope.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
