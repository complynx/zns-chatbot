# Native Telegram audio/video acceptance, 2026-10-15

Synthetic video and audio from the authorized account in `@complynx_test_bot`
passed the owned stand's Telegram intake, opaque file reference, native Loom
processing and final reply. This is a bounded processing check. It does not
establish unattended classification, speech understanding or production readiness.

## Environment and source handling

The bot used schema 27 / manifest `mvp-26`, application image
`sha256:6dc4c36c28b53db863999b84aebb5cd407232c6a9db5516c8bad9053605711e6`.
Loom and its worker were built from unmodified upstream commit
`1050d74eba2e2775c2196035a2c2611a4ece39f5`; see the
[upstream provenance and isolation](shuttle-loom-upstream-qa-2026-10-15.md).
The worker had no network, a read-only root, UID 10001, 512 MiB memory without
swap and bounded temporary storage. No developer stand or production service changed.

Files were generated locally with FFmpeg: a two-second 320 by 240 color pattern
with a 440 Hz tone, and a separate three-second mono PCM WAV tone at 16 kHz.
They contain no speech or personal content. Telegram Web received the original
files through its file chooser. The Music picker sent the WAV immediately without
a caption; the incoming update was verified as `audio`, not a document or voice
message. No second upload was made.

The bot persisted owner-scoped file IDs and metadata. Run input contained the
opaque ID; Loom fetched source bytes through the authenticated file API when
processing them. Both imported artifacts matched the local originals' exact
SHA256 and length: 70,241 bytes for video and 96,124 bytes for audio. This check
does not imply that Loom retains source files indefinitely.

## Processing and visible results

Video run `8da42e24-013e-4fdf-a174-678483df44f6` used file
`5d9ce772-436f-40d2-be77-392b7e4e98f1`. Native `video_info` returned two seconds,
320 by 240 video and mono 16 kHz audio. `video_frame` extracted the one-second
frame as an 11,369-byte JPEG. Pixel checks and visual inspection confirmed the
color pattern and timestamp. `video_audio` produced a 64,130-byte mono WAV with
two seconds of samples. The interior second measured 440 Hz; AAC startup padding
was excluded from this pitch check. Both derived artifacts were downloaded with
owner authorization.

Audio run `7079eaa7-cfc4-4822-80fa-ae43b332a24f` used file
`c995bf8e-7ddd-42c6-abeb-a8a093aa7cf1`. The same native metadata tool accepted
the audio-only source and returned three seconds, 16 kHz, mono, with no video
stream. This scenario did not perform transcription or infer speech content.

Both original bot tasks completed. Their Russian replies appeared in the same
Telegram chat, and were checked in the UI and a saved screenshot. Selected hashes
for passes, pairs, registrations, profiles, payment evidence/reviews and user/run
event context stayed unchanged. Task, attachment and outbox records changed as
expected. No payment or avatar operation was requested.

## Failures retained and recovery boundary

The first fixture export through Docker's archive-copy path failed even though
an exec read proved the worker file existed. Exact binary exec reads and matching
worker/host SHA256 recovered the files; temporary worker sources were then removed.
This was a harness export failure, not a Loom processing failure.

Two verifier assumptions were corrected: the frame output is JPEG, and whole-clip
zero crossings include AAC startup padding. The original native tool result was
retained and checked again; no media tool call was repeated to obtain a pass.

During those diagnostics the manual provider twice exceeded Loom's two-minute
inference timeout. The video run paused at `platform_retry_limit`. Its completed
media result and failed inference steps were retained. After verifying no pending
or unknown steps and a mock-only provider, an explicit owner resume completed the
same run with a stable idempotency key. Only the owned stand's sponsored attempt
limit temporarily changed from two to three. Original configuration bytes were
restored and Loom restarted successfully afterward. Exactly one media tool step
remained. This is an explicit test recovery, not proof of automatic retry-limit
recovery or an unattended provider pass.

The manual Codex scenario provider chose tools and authored final replies. IAM,
Telegram source download, broker, database and native worker were real. No paid
transcription was invoked. The existing Go source gates remain unchanged; this
slice changed only QA helpers, evidence and documentation.

## Review and retained evidence

An independent helper/recovery review found no actionable findings. Evidence lives
in ignored `.cache/shuttle-live-test/media-qa/`: `fixtures.json`,
`native-media-checkpoint.json`, both tool-result JSON files, `video-frame.jpg`,
`video-audio.wav` and `telegram-media-pass.png`. The recovery configuration backup
is private ignored evidence. Earlier failures are not rewritten as successful.

Two incomplete MP4 cases subsequently passed
[separate bounded native QA](shuttle-malformed-media-qa-2026-10-15.md). The original
failed expectation and actual container-only result remain retained there.
Voice/video-note processing subsequently passed a
[separate native check](shuttle-voice-video-note-qa-2026-10-15.md) on the newer
`mvp-33` image. That check retains the manual-provider and capture limitations.
At this original checkpoint, voice messages, video notes, oversized inputs, other malformed formats, paid
provider disclosure, OCR and unattended choice between payment proof, avatar and
unrelated media remain separate acceptance gates. Existing behavioral intake/access tests
do not substitute for those native checks. Retention policy remains open.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
