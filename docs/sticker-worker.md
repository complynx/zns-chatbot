# Isolated sticker worker

The optional sticker transport is separate from the audio/video media worker.
The bot calls `stickerclient.New(origin, secret)` and its `Normalize` method.
Native decoding runs only in the Unix-socket decoder container. The TCP broker
holds a dedicated bearer secret; neither component needs an OpenAI key.

From the repository root, set `STICKER_WORKER_SECRET` to a fresh private value:

```powershell
docker compose -f compose.sticker.yaml build decoder broker
docker compose -f compose.sticker.yaml up -d --no-build decoder broker
```

The broker listens on `127.0.0.1:8098`. Existing functional stands are not changed.
`sticker-native` is a build dependency and exits immediately if started. Native
FFmpeg and rlottie versions/base digests are pinned in `platform/Dockerfile.sticker`.
See [native limits and licensing](sticker-media.md) before distributing images.

`POST /v1/normalize?format=webp|tgs|webm` accepts raw bytes and
`Authorization: Bearer <secret>`. A successful response is an array of objects
with base64 `png`, integer `timestamp_ns` (source-relative nanoseconds), `width`
and `height`. Rejections contain generic errors, never native stderr or paths.
There are no paths, executable names, URLs or model options in the request.

Both the broker and decoder admit one job at a time and reject excess requests.
Input is capped at 1 MiB; request/client work at 20 seconds; native work at 15
seconds. Responses are capped at 12 MiB, four frames and 2 MiB per PNG. The broker
and bot client independently validate the frame count, bounded dimensions,
strictly increasing timestamps below three seconds, and complete PNG decoding
(including checksums and absence of trailing bytes). Static WebP requires one
frame at timestamp zero. HTTP redirects are never followed.

The decoder has no network, no credentials or host mounts, a private 0700 IPC
directory/0600 socket, a read-only root, a 32 MiB noexec tmpfs, 256 MiB memory,
one CPU, 32 PIDs, no capabilities and no-new-privileges. It clears its environment
at startup. Only the broker and decoder share the IPC volume and numeric UID;
the broker mounts it read-only. The shared IPC volume is a size-limited 1 MiB
tmpfs, so the decoder has no writable persistent disk. The broker image contains
no native codecs. The new volume name preserves any old unbounded development
volume without reusing it; old volumes can be removed after their containers stop.

Use a private service network or a trusted TLS ingress for remote callers. Do not
publish the broker unprotected or place unrelated containers on the IPC volume.
The supplied localhost binding is intended for a caller on the same host.

Focused verification:

```powershell
cd platform
go test ./internal/stickerclient ./internal/stickerworker ./cmd/stickerworker
./tools.local/golangci-lint.exe run ./internal/stickerclient/... ./internal/stickerworker/... ./cmd/stickerworker/...
```

These checks cover protocol validation, truncated/oversized PNGs, invalid
timestamps and frame counts, authentication, admission, input limits and redirect
rejection. Transport acceptance does not establish Telegram-like functional
acceptance or visual model quality. Sparse sampling may miss transient details.
