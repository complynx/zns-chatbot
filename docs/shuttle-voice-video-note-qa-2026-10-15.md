# Native Telegram voice/video-note acceptance, 2026-10-15

Synthetic forwarded voice and video-note messages passed the owned stand's real
Telegram intake, opaque file-ID handoff, local Loom processing and visible reply.
This closes these two media-type processing gates. It does not establish speech
understanding, microphone/camera capture or unattended attachment classification.

## Runtime and intake

The application image was
`sha256:8323e6f63d4b2a8967527378ea5dbff9eb7f7fa84076071ef528f64327040c7a`,
manifest `mvp-33`, database schema 32. Real local Zitadel, Loom and its networkless
bounded file worker were used. The current account 8968148043 forwarded both
fixtures only to `@complynx_test_bot` (6087685431). Actual persisted incoming
updates verified that sender, private chat, media kind and Telegram unique-file
ID matched the intended fixture.

FFmpeg generated a three-second Opus tone and a two-second 240 by 240 MP4 color
pattern with mono audio. They contain no speech or personal media. The bot sent
each fixture once to the authorized chat through `sendVoice`/`sendVideoNote`, then
Telegram Web forwarded each once back to that same bot. This exercises forwarded
native message types, not a browser upload interpreted as an ordinary document.
The documented methods are in the [Telegram Bot API](https://core.telegram.org/bots/api#sendvoice)
and [video-note API](https://core.telegram.org/bots/api#sendvideonote).

Each source was checkpointed before its one upload. There was no automatic
Telegram retry, duplicate forward or replacement run. Temporary source files were
removed from the worker after exact byte/hash export. No extra container was
created, and no Loom developer stand or production service changed.

## Processing and delivery

Voice task `d83a06a9-0138-4a22-bd2f-28dc419aa467` used run
`ddc9dbcd-6839-4923-b124-5617d35bbd2b` and opaque file
`8f2fe177-ae02-4493-ad89-c491a08a9c50`. Loom's native metadata tool returned
Opus, mono, approximately 3.0065 seconds. The imported artifact matched the original
12,855-byte source and its SHA256 exactly.

Video-note task `f9fa456a-4f86-4363-b1b7-6b8b84e872f3` used run
`e275b665-e682-4fcc-9e3e-0622002b1269` and opaque file
`a7d2fb68-96ee-4d3d-af3e-5c31fd190df5`. Native metadata returned two seconds,
240 by 240 video and mono audio. The imported artifact matched the original
60,200-byte MP4 and SHA256 exactly. The first-second frame was downloaded with
owner authorization; pixel checks and visual inspection confirmed a 240 by 240
JPEG with the expected color pattern and timestamp.

Each run has exactly one media script step, each bot task completed, and each
has one delivered outbox receipt for the authorized private chat. Both Russian
replies were observed in Telegram Web. Eight selected business tables retained
identical before/after hashes: passes, pairs, registrations, user profiles,
payment evidence/reviews and user/run event context. Attachment, task, history
and outbox records changed as expected.

The existing manual Codex mailbox provider selected the local metadata/frame
calls and authored final replies. No paid transcription, upload to an external model provider,
payment/evidence, avatar or profile mutation was requested. This is real-service
infrastructure acceptance, not an unattended model evaluation. No application code change,
new dependency, migration, image build or full Go-suite repeat was needed.

## Retained failures and review

The first voice-processing helper looked for the Telegram message directly in the
inbox payload. The actual durable envelope stores it under `raw_update`; the
helper was corrected and continued on the same already accepted task. No media
tool call or send had occurred in that failed phase. A final verifier initially
expected the outbox label `sent`; source/readback uses `delivered`. Its corrected
read-only check preserved the same receipts and did not repeat any operation.
These harness failures remain in the checkpoint.

Writing a browser screenshot directly to the workspace failed with EPERM. A
permitted temporary-file save followed by a guarded workspace copy retained the
screenshot; no Telegram action was repeated. The source files, raw tool results,
checkpoint, downloaded frame and UI proof are ignored local evidence under
`.cache/shuttle-live-test/voice-video-note-qa`.

A fresh independent review covered the helper. Its envelope correction and this
factual report receive a separate final review. Remaining media gates include
speech interpretation, unattended choice between payment proof/avatar/unrelated
content, production retention and other malformed formats. This is local QA,
not a production release. No commit or push was made.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
