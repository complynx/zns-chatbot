# Native incomplete MP4 acceptance, 2026-10-15

Two distinct synthetic files from the authorized Telegram account in
`@complynx_test_bot` exercised incomplete-media handling through the actual bot,
owner file API, native Loom broker and isolated file worker. This is manual
scenario-provider QA, not unattended attachment interpretation.

The bot used schema 27 / manifest `mvp-26`, image
`sha256:d2a10ad3189dd66ecb1c6a09ec373b77020c98f75a4eaf3e50dd54360e0c2ca8`.
Loom and the worker remained on the unmodified
[accepted upstream build](shuttle-loom-upstream-qa-2026-10-15.md).
No Go source, configuration, dependency or developer stand changed for this
media slice. At this acceptance checkpoint, the 228-file source freeze matched
the earlier 184-scenario race/lint gate; that gate was not rerun for these QA
helpers and documents. Subsequent task-closure work has its own checks.

## Container metadata without usable streams

The first file was the first 64 bytes of the previously accepted synthetic color
pattern video. Telegram sent it as a document with an explicit test caption. Loom
imported it through the owner-scoped opaque ID, not at run creation. One script
called `video_info` on the known-good two-second video, the incomplete file, and
the known-good file again.

The initial verifier expected an error and failed. Actual `video_info` returned
an MP4 container format and size 64, with no streams or duration. The control
video succeeded before and after. Native `ffprobe` also exited zero for these
64 bytes; Loom's implementation returns that probe metadata. A successful metadata
response alone therefore does not establish usable media. No new Loom defect was
established, and no speculative upstream issue or bot-side media parser was added.

The original failed checkpoint and tool result remain unchanged. The same run
was finished with an honest Russian reply: only container metadata was readable,
no usable streams/duration were found, and a complete file could be supplied.
No second processing call or replacement Telegram request was used for this case.
The exact 64-byte source download matched SHA-256
`4511303d99f21a08d3e52b1103a2573421c3f937a42f2ed47bd7890870be6ec7`.

Run: `6a9aa425-cf6e-4148-a321-ecf06ea938e8`.
Opaque file: `771e2985-f3b8-4e31-b186-aa0aa5e9d6b5`.

## Rejected incomplete header

A separate eight-byte `ftyp` header used its own filename, caption, task and run.
A bounded native worker precheck confirmed this header made `ffprobe` exit
nonzero, while the 64-byte container still exited zero. Both prechecks used
synthetic bytes over stdin and changed no worker files.

Through the actual Telegram/Loom path, exactly one invalid `video_info` call
returned the normalized `file processor unavailable or invalid bounded request`
error. The same script successfully inspected the known-good video before and
afterward. The worker remained running without OOM. No streams or derived outputs
were claimed from the rejected input. The manual provider delivered a Russian
reply explaining that video metadata could not be read and a complete file was
needed. The source remained accessible through its owner-authorized artifact:
the downloaded eight bytes matched SHA-256
`3b47a05cc0f7b533587c3d898599fbff587465e8e43547090b24e9bdacafcd68`.

Run: `9e8fccbe-5d56-44fd-9da5-0b159a01d9f9`.
Opaque file: `ba723a20-5679-49f3-a230-a5074a78a357`.

## Preservation, review and limits

Fresh database readback confirmed both original bot tasks completed and each run
retained one original script tool step. Eight selected business-table hashes
matched before processing, after processing and after delivery: passes, pairs,
registrations, profiles, payment evidence/reviews and user/run event context.
No payment, evidence, avatar or profile write was requested. Actual inbox, task,
attachment and outbox records remain as test provenance. Both replies were
visually checked in the same chat; cropped screenshots exclude the other chats.

Helper reviews found one missing comparison between downloaded bytes and the
expected SHA-256 in the first finisher. The helper was fixed, and a separate
read-only check verified the already downloaded file's exact bytes against both
stored digests without resending or rewriting the failed checkpoint. A fresh
review found no actionable findings. The final acceptance audit rechecked both
source downloads, original failure digest, completed tasks, original script steps,
business hashes and current image. No new containers were created.

Ignored evidence is under `.cache/shuttle-live-test/malformed-media-qa/`:
`checkpoint.json` (retained failed error expectation), `tool-result.json`,
`container-only-recovery.json`, exact source/downloaded files, and
`telegram-container-only.png`. The separate `rejected-header/` directory contains
its successful checkpoint, tool result, native probe outputs, source/download
and `telegram-rejected-header.png`.

These checks cover two small incomplete MP4 inputs. They do not establish support
for all malformed formats, oversized inputs, voice/video notes, transcription,
unattended classification or production retention. The model choices and replies
were authored manually; transport, IAM, broker, byte retrieval and worker execution
were real. The original rejected-error assertion remains a failed assertion.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
