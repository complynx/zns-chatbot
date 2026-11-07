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
