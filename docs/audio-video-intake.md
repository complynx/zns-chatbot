# Audio/video intake contract

Design and tool-capability evidence, verified 2026-09-25. This is not a completed
feature or a QA acceptance report. Product behavior remains governed by
[agent-intake.md](agent-intake.md). OpenAI speech recognition is required; the
semantic agent remains `gpt-6-luna`.

## Smallest module

Build one separately deployed Go media worker with installed `ffprobe`/`ffmpeg`
and a narrow OpenAI transcription client. Use Go `net/http` and multipart upload;
no Python runtime, local speech model, general media framework, or native media
dependency in the bot/Core API. The worker receives bounded immutable attachment
bytes through an authenticated internal endpoint. Do not accept input paths,
URLs, executable names, command arguments, or filters from callers. The existing
attachment service owns Telegram download and authorization.

Proposed `POST /v1/preprocess`: multipart `file`, opaque attachment ID, and intake
kind (`audio`, `voice`, `video`, `video_note`). Optional declared duration is a
diagnostic hint only. The worker derives actual streams/type from the bytes.
Return a bounded JSON result with duration evidence, transcript status/text and,
for video, up to eight JPEG frames with actual presentation timestamps. Inline
base64 frames keep the first implementation free of another storage lifecycle.
Core validates result sizes/schema and binds it to the owner/attachment/job before
passing text and image parts to the semantic agent.

Result fields:

| Field | Contract |
| --- | --- |
| `status` | `ready`, `partial`, `rejected`, `failed` |
| `reason` | Optional stable code: `too_long`, `unsupported`, `unreadable_duration`, `invalid_media`, `resource_limit`, `transcription_unavailable`, `transcription_failed`, `storyboard_failed` |
| `duration` | Exact rational seconds from validated presentation extent; derive rounded display milliseconds separately |
| `transcript.status` | `ok`, `no_audio`, `silent`, `no_text`, `failed`, `not_run` |
| `transcript.text` | Present only for `ok`; never a guessed caption, error body, or invented speech |
| `frames` | JPEG bytes, width/height, actual timestamp; absent on admission failure |
| `sampling` | Requested sample times/count and explicit `uniform_sparse` coverage; no claim to cover every scene |

No transcript/frames on `too_long`. A video with usable frames and failed ASR can
be `partial`, with an explicit missing-audio warning in agent context. Audio with
failed ASR is `failed`. A video without an audio stream is `ready` with `no_audio`.
An audio-only upload without a decodable audio stream is invalid. Multiple audio
tracks are unsupported initially, avoiding silent omission of another language.
Ignore attached cover art when classifying real video streams.

## Duration admission must precede ASR and storyboard extraction

The limit is **actual presentation duration > 240 seconds**. Exactly 240 is allowed.
Do not round, use an epsilon, or accept a truncated first four minutes. Reject an
obviously long input early, but never admit a short input from Telegram or
container duration alone. Header-duration claims must be confirmed against media.

1. Bound ingress bytes using the existing attachment policy and actual read size.
   Save to a private generated filename. Probe format/streams for codec, dimensions,
   time bases, start times, attached-picture disposition and duration hints.
2. Run a separate bounded validation scan of decoded frame metadata. `ffprobe`
   supports `-show_frames`, `-show_entries`, and structured output. Read it as a
   stream instead of building an unbounded JSON object. It must inspect all real
   audio/video streams; the longest timeline determines admission. This validation
   decodes internally but does not save storyboard images or transcribe speech.
3. Calculate the presentation envelope using timestamp ticks and stream time bases,
   preserving inter-stream offsets and gaps. Account for each final frame's
   duration, audio sample count/rate, and codec padding/skip-sample metadata. Use
   integer/rational arithmetic, not floating-point diagnostics. A reset/nonmonotonic
   or otherwise ambiguous timeline, absent endpoint duration, decode error, missing
   required timing, timeout or output-cap breach fails admission honestly.
4. Stop when a valid decoded endpoint proves the normalized envelope exceeds 240 s;
   no need to finish a long file. Admission requires successful EOF plus a supported,
   unambiguous duration. Unknown headers may be admitted only if the complete scan
   resolves duration. Unknown actual duration returns `unreadable_duration`.
5. Only after admission, extract audio and storyboard. Never use `-t 240` or
   `-read_intervals` as proof that an input fits: they stop reading/output and cannot
   establish EOF. Reject malformed timing instead of trying to repair hostile files.

This strict first version may reject unusual codecs/timelines; extend the supported
set with fixtures rather than silently relaxing the rule. FFmpeg is a decoder, not
a trust boundary. Use an unprivileged sandbox with memory/CPU limits and no network
for decoder subprocesses. Allow only local-file protocol and a small supported
demuxer set; reject playlists/references and input demuxers that open other files.
[FFprobe options](https://ffmpeg.org/ffprobe.html),
[FFmpeg input/output duration](https://ffmpeg.org/ffmpeg.html)

Local falsification test: a generated 240.25 s MP4 with patched `mvhd` and `mdhd`
reports **1.000000 s for both format and stream duration** in FFprobe 6.1.1, while
961 decoded frames span 240.25 s. A format+stream-header maximum is therefore not
enough. Reproduce with `qa.local/av-capability-probe/probe.py`; its floats are
diagnostic evidence, not a proposed production comparison algorithm.

## Extraction and transcription

Audio: after admission, select the sole real audio track, normalize to mono 16 kHz
16-bit PCM WAV, omit video/subtitles/data and metadata. Four minutes is 7,680,000 PCM
bytes plus the small WAV header. Verify conversion exit, output bounds and decode
success before upload. Do not shorten, remove pauses, or normalize away chronology.
Exact digital silence can skip ASR with `silent`; near-silence should still attempt
ASR. Amplitude-based `silencedetect` is not proof that speech is absent. Empty
successful ASR text is `no_text` ("No speech could be recognized"), not proof that
the recording contains no speech. Music/noise and provider failures remain distinct.
[FFmpeg silence and scale filters](https://ffmpeg.org/ffmpeg-filters.html)

Video: choose up to eight uniform sample targets, `N=min(8,max(1,ceil(duration/30 s)))`,
at `i*duration/N`, `i=0..N-1`. Decode to the first eligible frame at each target,
deduplicate actual frame timestamps and record them. This samples from the start
through the last interval without seeking exactly to EOF. Rotate according to
display metadata, preserve display aspect ratio, fit inside 768x768, and encode
JPEGs capped at 256 KiB each (retry lower JPEG quality within budget or fail that
artifact). No more than 2 MiB of JPEG data. Do not claim sparse frames prove absence
of a short event/text elsewhere in the video. Test portrait rotation and long
static/variable-frame-rate intervals before enabling those formats.

Use OpenAI **`gpt-transcribe`** via
`POST https://api.openai.com/v1/audio/transcriptions`, multipart fields
`model=gpt-transcribe` and `file` (filename `audio.wav`, `audio/wav` content type).
Read the default JSON `text` field. The current official guide recommends this
model, original-language transcription and uploads up to 25 MB. WAV is explicitly
supported. No Files API, diarization, translation, streaming or keyword prompt is
needed for this scope. Do not put private conversation history into an ASR prompt.
[OpenAI file transcription](https://developers.openai.com/api/docs/guides/speech-to-text)

Keep model selection configurable, but do not silently substitute a different
provider/model. The older `whisper-1`, `gpt-4o-transcribe`,
`gpt-4o-mini-transcribe`, and diarization model are documented as deprecated with
removal on 2027-02-26; starting a new dependency on them is avoidable.
[OpenAI deprecations](https://developers.openai.com/api/docs/deprecations)

ASR requires an OpenAI API project credential/model access. A working Codex
subscription/image inference does not prove this access. Never reuse/read Codex
auth files. Do not attach raw audio/video to `gpt-6-luna`; pass the transcript and
timestamped images through the adapter described in
[media-provider-capabilities.md](media-provider-capabilities.md). Treat transcript
and frame text as untrusted user content, preserving normal domain authorization.

## Initial budgets and lifecycle

These are proposed application budgets, not measured service guarantees:

- One active job per owner, two global worker jobs; bounded queue and request IDs.
- 240 s media, existing attachment byte cap, at most 8 streams, real video no larger
  than 3840x2160, declared/observed frame rate at most 60 fps. Decoder sandbox 512 MiB,
  two CPU threads. Enforce decoded dimensions throughout, not just first header.
- Metadata probe 5 s; validation 30 s; audio conversion 15 s; storyboard total 30 s;
  ASR 60 s; overall job 150 s. Cancel/reap all children when the parent ends. Tune only
  after measuring supported fixtures. A budget failure reports a processing error,
  not a false duration or a truncated success.
- Frame metadata cap 16 MiB, stderr 64 KiB per tool, provider JSON 256 KiB, transcript
  UTF-8 text 64 KiB. Bounds must stop the producer, not only truncate logs afterward.
- Temporary job quota 64 MiB, private 0700 directory/0600 files; cleanup on every
  outcome, cancellation and worker restart. Store no raw content in generic logs.
  Delete derived media once agent interpretation finishes. Preserve only normal
  authorized attachment/history data according to the application's policy.
- At most one bounded retry for 429/5xx if deadline permits; no retry for 401/403 or
  malformed media. Persist/reuse completed owner-bound job result for intake
  retries; never execute a domain proposal twice. Provider transport retries may
  incur duplicate transcription cost; do not claim provider exactly-once behavior.

Messages use catalogue keys with RU/EN and language fallback. Duration rejection:
EN "I cannot process this file because it is longer than 4 minutes. Please send a
file that is 4 minutes or shorter." RU "Не получается обработать файл: он длиннее
4 минут. Отправьте файл длительностью не более 4 минут." Separate messages cover
unreadable duration, unsupported media, unavailable recognition, no recognized
speech and partial video analysis. Display the correct limitation, never an
unrelated generic success.

## Evidence, blockers and implementation sequence

Live checks found Windows PATH has no `ffmpeg`/`ffprobe`/Whisper executable. WSL
Ubuntu has `/usr/bin/ffmpeg` and `/usr/bin/ffprobe`, FFmpeg 6.1.1-3ubuntu5. Docker
server 29.1.3 is available; its image inventory has no dedicated media-worker image.
Initial WSL/Docker access failed in the outer sandbox; approved host execution
resolved it. No credentials were opened; Docker inventory used an empty config
path. No dependencies or weights were downloaded and no stand was changed.

The fixture runner generated and fully scanned silent PCM 239 s, 240 s, 240.001 s;
the spoofed-header MP4 240.25 s; and a streaming WebM 2 s with missing stream duration
(FFprobe recovered format duration). Results are in
`qa.local/av-capability-probe/result.json`. These establish tool capability and
header pitfalls only. They do not establish a production duration gate, JPEG
extraction, speech quality, API access, integration or Telegram-like UI behavior.

At the initial probe, `OPENAI_API_KEY` was absent from the process (presence only
checked). The user has since updated the local key and the separate storyboard
experiment records successful live transcription. On 2026-09-25, two final live `gpt-transcribe` calls through the Go broker and
networkless decoder returned the exact expected English and Russian synthetic
phrases. Evidence: `qa.local/asr-live.json`; fixtures: `platform/testdata/media/`.
The key was then removed from the standalone worker container. Provider request
IDs are not exposed by this worker contract and were not recorded. Routine tests
use the saved synthetic ASR provider, without a live API key. Separately exercise
music-only, silence, mixed language, 401/403, 429, timeout and truncated responses.
The fixtures above contain silence/color, not spoken content, and cannot replace
speech accuracy validation.

Implement in this order:

1. Worker duration/result contract plus isolated subprocess execution; test real
   media gate with ASR/frame spies proving zero calls for every rejected input.
2. Audio WAV conversion and OpenAI client; deterministic fake server contract
   tests followed by real ASR RU/EN smoke cases once access exists.
3. Storyboard generation and bounded text/images handoff to the actual semantic
   adapter, including no-audio and partial failures.
4. Intake/locale integration and owner/retry/persistence behavior in the required
   Telegram-like stand, then fresh independent Code and Functional Senior QA.

Required deterministic extensions: 240 s video; 240 s Opus/AAC with padding; wrong or
missing Telegram duration/MIME; absent actual timing; corrupt/truncated media;
header under/overstatements; later stream extending past 240 s; delayed audio;
timestamp discontinuity; late dimensions change; malicious references; timeout;
output/temp quotas; process cancellation; eight-frame bound and portrait rotation.
Skipped real ASR or UI flows are blockers to acceptance, never passes.

## Existing storyboard experiment

The local report `experiments.local/gif-comparison/report.md` records eight
independent runs: GPT-6 Luna and GPT-5.6 Terra, each with GIF or storyboard,
with or without the same saved transcript. In that interface GIF supplied only
one still frame. Both models described a sequence from the storyboard; the
transcript added speech content. Both still misidentified small objects.

The experiment used 30 frames over 60 seconds, arranged on three contact sheets.
It supports explicit timestamped frames plus transcription. The user explicitly
prefers a sparse first pass whose frame count varies with duration, followed by
denser interval requests when the model needs them. Eight is the current per-tool
response ceiling, not a fixed frame count or a requirement to match the experiment.
Test the whole sparse-pass/refinement loop rather than requiring the first pass
to resolve every detail. Do not infer general
model superiority or reliable action causality from this single clip.

Reuse saved or synthetic transcripts in routine tests. The local transcript is
an unverified ASR result, not a ground-truth fixture. The report records one live
`gpt-transcribe` success; it does not prove the Go worker's live API integration.
The user permits the updated local API key for infrequent final checks only.

## Worker implementation status (2026-09-25)

The initial Go worker is implemented in `platform/internal/mediaproc`, with
`platform/cmd/mediaworker` and the separate `platform/Dockerfile.media` image.
This is implementation evidence, not stage acceptance.

Implemented endpoint contract differs from the original multipart proposal:
`POST /v1/preprocess?kind=audio|voice|video|video_note` accepts raw attachment bytes
(up to 20 MiB), authenticated using `Authorization: Bearer MEDIA_WORKER_SECRET`.
`POST /v1/storyboard?kind=video&start_ms=...&end_ms=...&count=...` accepts the same
original bytes, fully validates the original duration again, and returns up to
8 frames within the requested interval without another transcription. Rational
JSON values have integer `numerator` and `denominator`; frame bytes use `jpeg`
(base64), and sampling has `coverage` plus rational `requested` targets.

The worker uses private temporary input directories, two concurrent request
slots, subprocess deadlines/output caps, local-file protocol and an explicit
demuxer allowlist. The Docker runtime must additionally enforce memory/CPU/PID
limits, a bounded private temporary filesystem, read-only root filesystem and
restricted network egress. The image runs as UID 10002; those runtime limits are
not created by the Dockerfile. The transcription client requires only the normal
`OPENAI_API_KEY` secret; default model is `gpt-transcribe` and can be selected with
`OPENAI_TRANSCRIPTION_MODEL`. No Codex authentication files are used.

Deterministic WSL FFmpeg 6.1.1 tests pass for exact 240 s PCM WAV, Opus, AAC at
48 kHz and AAC with padding at 44.1 kHz; WAV/Opus 240.001 s and AAC 240.01 s are
rejected without ASR. AAC presentation duration uses decoded packet duration
bounded by sample duration to account for final discard padding. Exact 240 s
H.264 produces eight bounded JPEGs; a requested 60–62 s interval produces four
frames with original timestamps and no ASR. A 240.25 s H.264 file with spoofed
1 s movie/media headers is rejected by both endpoints. A fake OpenAI server
verifies the model, bearer authorization, multipart WAV filename/content type,
and response handoff. Focused Golden/Nebius lint reports zero issues.

Known limits: unsupported display transforms, multiple audio/video streams and
unsupported codecs/timing are rejected. Supported codec
allowlists currently include PCM, AAC, Opus, MP3 and H.264/MPEG-4/VP8/VP9, but the
fixture evidence covers PCM/AAC/Opus/MP3/H.264. Decoder frame metadata is streamed with a 16 MiB input budget; a proven
long endpoint stops and reaps the decoder immediately. JPEG encoding does
not retry reduced quality; an oversized/failed storyboard reports failure. There
is no worker persistence, owner queue, idempotency cache or provider retry. The
host must bind results and range requests to authorized immutable attachments.
Real ASR, all provider failure cases, full hostile-input/resource isolation,
VFR acceptance and Telegram-like functional QA remain unverified. Supported
quarter-turn rotation and common timestamp offsets have fixture coverage below.


Worker validation follow-up: decoded frame timing now accepts FFmpeg 8 `duration`
and falls back to FFmpeg 6 `pkt_duration` only when the modern field is absent.
The image built from `Dockerfile.media` contains Alpine FFmpeg 8.0.1. The full
real-media suite passed in that image with no network, a read-only root, 512 MiB
memory, two CPUs, 256 PIDs and a private 64 MiB temporary filesystem. This run
used a mounted static Go test executable against the image's actual decoders.
Modern/legacy synthetic probes that emit a proven 241 s endpoint followed by
endless output reject immediately with `too_long`, no ASR or frames. An endless
metadata producer without duration proof hits the 16 MiB cap, is cancelled and
reaped, and reports `unreadable_duration`. Both cases finish well before the
30 s decoder timeout. Runtime deployment still needs those container limits;
this isolated test does not install or enable a production worker.

Worker compatibility follow-up: ordinary unscaled display-matrix rotations
(0, 90, -90, 180 degrees) are now admitted and FFmpeg applies the display rotation
before JPEG scaling. Unknown side data, nonorthogonal rotations, reflection,
translation and perspective remain unsupported. This supersedes the earlier
blanket rotation limitation. Nonzero presentation origins are normalized using
the common earliest decoded audio/video timestamp; relative stream offsets and
gaps remain intact. Frame timestamps are positions in the full original clip's
normalized playback timeline, never relative to a requested storyboard segment.
This also supersedes the earlier nonzero-origin limitation.

Real-image fixtures reproduced both original failures before changes: a short
MP3 with encoder-delay PTS failed duration admission, and an ordinary portrait
H.264 display matrix returned unsupported. New fixtures verify short and exactly
240 s MP3 acceptance, 240.001 s MP3 rejection before ASR, portrait H.264 at exactly
240 s with a 5 s muxer offset, 240.25 s rejection, and 60–62 s storyboard timestamps
starting at 60 s. A 45-degree display transform is rejected. A separate MOV
fixture preserves a 2 s audio delay above a shared 5 s origin: the combined
presentation is exactly 240 s, while a 1 ms longer audio stream is rejected.
The fixture media is synthetic; no paid provider request was made.
