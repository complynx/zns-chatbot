# Synthetic speech fixtures

These recordings contain no real user speech. Generated offline with Windows
System.Speech at mono 16 kHz, signed 16-bit PCM, using installed voices:

| File                    | Voice                   | Exact text                                      |
| ----------------------- | ----------------------- | ----------------------------------------------- |
| `orders-en.wav`         | Microsoft David Desktop | Please show my orders.                          |
| `help-ru.wav`           | Microsoft Irina Desktop | Какие услуги я могу забронировать?              |
| `receipt-second-en.wav` | Microsoft David Desktop | This receipt is for the second order.           |
| `profile-en.wav`        | Microsoft David Desktop | My full legal name is Taylor Synthetic Example. |

The receipt-choice and profile recordings use synthetic expected transcripts;
they were not sent to live ASR. They exercise semantic domain actions in the
local real-model stand without provider calls. Profile tests must restore the
previous synthetic profile after their run.

`later-amount.mp4` is a synthetic silent 20-second H.264 video: the first ten
seconds show "Beginning of synthetic video", then "35 BYN". A sparse initial
frame cannot reveal the later amount; request the second half to test refinement.
`too-long.mp4` is a synthetic blue 240.25-second clip. It must be rejected even
when the Telegram duration hint is zero or short. Both use generated graphics,
contain no user data and need no transcription provider.

The fake Telegram service provides a synthetic OpenAI-compatible transcription
endpoint at `/lab/asr`. It matches the SHA-256 of PCM sample bytes against
`internal/sandbox/asr_fixtures.json`; it does not recognize speech. Unknown input
returns HTTP 422 instead of an invented transcript. WAV headers from FFmpeg may
vary; PCM must match exactly. Routine checks use these saved outputs without paid
provider calls. Keep source WAV lossless when testing this fixture provider.

Start the complete deterministic stand from `platform`:

```sh
docker compose -f compose.yaml -f compose.qa.yaml -f compose.av.yaml up -d --build --wait
```

The overlay connects the real isolated decoder and transcription broker to this
fixture provider on an internal network. It explicitly replaces the API key with
a synthetic marker and removes the broker's host port. No ambient real key is
used. The decoder has no network and no credentials. All existing PostgreSQL
volumes remain intact.

In the Telegram-like UI choose Audio or Voice, upload one of the WAV files, and
send. Declared seconds are deliberately editable, including zero, so QA can
confirm that the worker trusts actual decoded duration. Video and Video note
uploads use the same bounded file transport. The sandbox stores the original
bytes; it does not imitate Telegram's codec conversion. Malformed files can be
uploaded deliberately to exercise worker refusal.

`GET /lab/asr/state` with `X-Sandbox: 1` reports recognized fixture calls since
the fake service started. Use the counter to prove successful preprocessing is
reused on retry; restarting the fake service resets this diagnostic counter.
Actual OpenAI compatibility and accuracy need a separate infrequent final check.

For the native Codex stand, add `-f compose.av.qa.yaml` to expose this synthetic
broker on loopback port 8097. Launch `scripts/model-codex.mjs` with
`MEDIA_WORKER_URL=http://127.0.0.1:8097` and
`MEDIA_WORKER_SECRET=sandbox-only-media-broker`. Its model is real local Codex;
its transcription remains the exact-fixture provider. This is separate from the
optional standalone media worker on port 8096 used for real-provider checks.
