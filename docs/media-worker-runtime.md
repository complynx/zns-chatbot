# Isolated media worker runtime

This optional Compose project is separate from the main sandbox. It exposes only
`127.0.0.1:8096` and does not rebuild or reconfigure ports 8090 or 8093.

## Process boundary

- `broker`: unprivileged Go HTTP service, bearer authentication, bounded Unix RPC
  client and OpenAI transcription client. It has no FFmpeg executable. Only this
  container receives `OPENAI_API_KEY` and `MEDIA_WORKER_SECRET`.
- `decoder`: unprivileged Go service with FFprobe/FFmpeg. It has no network
  interface except loopback, no provider/broker credentials, and a separate PID
  namespace. It listens only on `/run/media-ipc/decoder.sock`.
- A project-specific volume shares that Unix socket. Directory mode is 0700,
  socket mode is 0600; both containers use UID 10002 in separate namespaces.
  The broker mounts this volume read-only. The internal bearer string is a
  protocol marker, not a secret; filesystem permissions enforce IPC access.

The decoder has a read-only root, all capabilities dropped, no-new-privileges,
512 MiB memory, two CPUs, 256 PIDs and a private 64 MiB noexec/nosuid tmpfs. Its
child processes get only `LANG=C` and `LC_ALL=C`. Each request uses private generated
files, cleans them on completion/cancellation, and reaps decoder children.
Startup removes abandoned `media-*` job directories from the decoder's private
tmpfs. The broker is limited to 256 MiB, one CPU and 64 PIDs.

The binary defaults to broker mode. `MEDIA_WORKER_MODE=decoder` selects the
Unix-only decoder and refuses startup if either provider or broker secrets are
present. There is no production mode that combines ASR credentials with native
media decoding. Local Go tests may still construct an in-process worker.

## Run

From `platform`, set a private `MEDIA_WORKER_SECRET` and optionally inject
`OPENAI_API_KEY` through the normal environment secret mechanism, then run:

```sh
docker compose -f compose.media.yaml up -d --build
```

No API key is required to test admission, silence, no-audio video and storyboard
ranges. Speech without a key reports `transcription_unavailable`; usable video
frames remain a `partial` result. Do not pass credentials to the decoder service.
`OPENAI_TRANSCRIPTION_MODEL` defaults to `gpt-transcribe`.
`OPENAI_TRANSCRIPTION_URL` is an operator-only test hook; its default is the
OpenAI transcription endpoint. Request fields cannot select a provider, socket,
URL, executable or input path.

The external contract is unchanged: authenticated raw bytes at
`POST /v1/preprocess?kind=audio|voice|video|video_note`, and range requests at
`POST /v1/storyboard?kind=video&start_ms=...&end_ms=...&count=...`.
Admission of the entire original file precedes extraction and ASR. Exactly
240 seconds is admitted; longer input contains no transcript or frames. Range
requests never call ASR. Both services admit at most two concurrent requests.

Decoder RPC responses are capped at 16 MiB; WAV is capped at 8 MiB and checked
for mono 16 kHz 16-bit PCM format, allowing bounded codec/resampling padding after
source-duration admission. Up to eight JPEGs are checked for size, dimensions and
full-clip timestamps. Decoder text is never accepted as a transcript. The broker
cancels Unix requests with the caller, refuses redirects, and uses only the fixed
operator-configured Unix socket.

## Verification

`go test -race -count=1 ./internal/mediaproc` in the repository's Linux test image
includes real FFmpeg fixtures and a fake OpenAI server. Tests cover Unix transport,
exact AAC padding boundaries, long-file rejection before ASR, no-ASR ranges,
malformed/oversized decoder responses, cancellation, rotation, encoder offsets,
secret-free child environments and original timing checks. No paid calls are
needed for these tests.

Inspect runtime configuration without printing environment secret values:

```sh
docker inspect zns-media-isolated-decoder-1 --format '{{json .HostConfig}}'
docker exec zns-media-isolated-decoder-1 ls /sys/class/net
docker exec zns-media-isolated-decoder-1 ls -l /run/media-ipc/decoder.sock
```

Expected: network `none`, only `lo`, 536870912 memory bytes, 2000000000 nano CPUs,
256 PIDs, read-only root and a private socket. The broker image must not contain
`ffmpeg` or `ffprobe`. The two containers must not use host PID or network modes.

The dedicated project can be stopped with
`docker compose -f compose.media.yaml down`; its IPC volume contains no media or
credentials. A socket volume created by an older local experimental build may
need its directory ownership/mode corrected to UID 10002 and 0700 before startup.
Do not mount a host Docker socket, host filesystem or general proxy into decoder.

The current local evidence used synthetic fixtures and a fake transcription
server. Real provider accuracy and end-to-end Telegram acceptance remain separate
checks. Network isolation is supplied by this Compose runtime, not by FFmpeg's
protocol allowlist alone.
