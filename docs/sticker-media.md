# Sticker asset normalization

`platform/internal/stickermedia` accepts downloaded bytes and a closed format enum.
It returns actual PNG frames with source-relative timestamps. It does not download
files, interpret placeholder emoji, call models, transcribe audio, or cache descriptions.
Call it only in the isolated media decoder; never in the bot or API process.

The trusted deployment config supplies FFmpeg, FFprobe, TGS renderer and temporary
directory paths. Request data cannot specify a path, URL or executable.

Limits: 1 MiB input; 2 MiB inflated TGS JSON; 512 by 512 pixels; 3 seconds and
180 decoded frames; four sampled animation frames; one static WebP frame;
2 MiB PNG per frame; 256 KiB probe output; 15 seconds total subprocess work.
Each native subprocess receives a minimal environment without credentials.
FFmpeg uses explicit demuxers and a file/pipe protocol whitelist, one codec/filter
thread, and no audio output. Private temporary input files are removed on all exits.
TGS JSON permits vector shapes and precompositions, rejects image/font resources,
expressions and excessive nesting, and is canonicalized before native parsing.
Admission reads the exact lowercase metadata keys from that same object; uppercase
lookalikes cannot override the dimensions or timing used for resource checks.
WebM extraction explicitly uses libvpx-vp9 to retain auxiliary alpha. Admission
requires a positive decoded-frame duration and checks every frame's end against
the three-second limit, including the final frame. Missing durations are rejected.
Frames preserve alpha. Sampling uses evenly spaced source frame indices; timestamps
come from actual WebM decoded frame PTS or TGS frame rate, not invented durations.

Runtime isolation is mandatory: no network, read-only root, no mounts containing
secrets, bounded tmpfs, memory and PID limits, no capabilities, one admitted request
at a time. Application byte/time limits do not replace native decoder containment.

Build and exercise the dedicated test image (does not change any functional stand):

```powershell
docker build -f platform/Dockerfile.sticker -t zns-sticker-test platform
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --memory 256m --cpus 1 --pids-limit 32 --tmpfs /tmp:rw,nosuid,size=32m zns-sticker-test
```

The image pins base digests, FFmpeg version and the Telegram rlottie source commit.
The pinned rlottie revision needs the standard `<limits>` header explicitly included
and two nonnegative color-count comparisons cast to `size_t` for modern Clang.
Warnings remain errors. Our small C++ adapter also uses GCC's static analyzer and
conversion warnings. The runtime image omits rlottie's image-loader plugin.
rlottie is LGPL-2.1-or-later; redistribute its corresponding source and license when
distributing the runtime image, following the dependency's license requirements.

The native tests generate a real WebP and WebM using FFmpeg and a gzipped vector
animation. The image runs native tests serially to match decoder admission and
avoid concurrent fixture encoders exhausting the shared PID limit. Tests use an
animation with a moving solid layer. They verify rendered pixels change with the TGS
timeline, sparse timestamps, cleanup, malformed inputs, external resources, gzip
bombs, size caps, subprocess output caps and cancellation. WebM regression fixtures
verify 25% alpha survives normalization and reject a four-second final frame whose
start timestamp is zero. Outside this image the
native test explicitly skips; that skip is not native-rendering acceptance.

This module is a bounded normalization slice. Bot wiring, description caching,
model interpretation and Telegram-like functional acceptance are separate work.
Sparse frames cannot prove every transient animation detail was observed.
