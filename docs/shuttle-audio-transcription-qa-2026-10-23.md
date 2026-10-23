# Audio transcription consumer acceptance, 2026-10-23

One synthetic attachment now passes through the actual Shuttle file API,
Loom native tools, isolated file worker and a local TLS transcription peer.
This accepts the consumer contract. The transcription text is a deterministic
fixture response, not a measurement of speech recognition or model judgment.

## Additional real-provider probe

After this synthetic consumer proof, Daniel selected one local Opus recording
and explicitly authorized its transmission to OpenAI. Exactly one POST to
`https://api.openai.com/v1/audio/transcriptions`, model `gpt-transcribe`, returned
HTTP200. The unchanged native Loom transcription adapter accepted both the text
and the provider's duration-based usage receipt. Source audio was9.1265 seconds;
the provider reported10 seconds. This is a provider usage unit, not a measured
price or an exercised Shuttle credit tariff.

The selected recording was normalized offline to mono16kHz PCM WAV using the
accepted worker image `856d5968`, with networking disabled. Source and WAV hashes,
raw provider receipt and transcript remain private. The transcript was shown to
Daniel. An exclusive fsynced admission record preceded network dispatch; the
single-request allowance is exhausted and no retry occurred. The initial tool
approval rejection happened before process execution, consumed no request, and
remains preserved alongside the later explicit consent and completed receipt.

This proves real-provider compatibility through the exact native adapter on one
recording. It is separate from the synthetic full consumer path below and does
not claim an additional full Telegram-to-provider run or broad speech accuracy.
Real Telegram ingress/login and human acceptance remain pending.

## Exact versions and path

- Shuttle source `035f2a6b0356effff75f3dfce0c776d7551c706a`, local image
  `sha256:bf1383e85fafd2d328437d1787420cb778c2b4f90ddd01806f62f4c8361a5fbe`.
  The standard Dockerfile used a tracked-only archive; Linux/amd64 and
  `10001:10001` were checked. This image was not published or deployed.
- Loom source `ba1f4a11237a07edb242735b09edb210e2a7209e`, API image `cbba225f8491`,
  file-worker image `856d5968d9d0`; IAM4.20 image `9ea0e9ecfdc4`.
  Relevant audio/artifact/schema source paths are unchanged between the retained
  `70e2c5eb` source snapshot and published `ba1f4a11`, verified through their
  complete six-commit GitHub comparison. The snapshot is not relabeled as ba1.

The fresh stand used real local IAM, Shuttle, Loom and worker processes.
Telegram and the Responses/transcription endpoints were controlled peers.
Certificates were verified against a new stand-local CA. Service networks were
internal; a separate fixed-destination TCP gateway provided loopback access
without terminating TLS. The transcription peer denied outbound connects and
loaded no real provider credential. The worker had no network, UID10001,
read-only root,512MiB memory/no swap, one CPU and128 PIDs.

The original WAV was one second, stereo8kHz PCM,32044 bytes. One Telegram audio
input produced an owner-scoped opaque Shuttle attachment. Three locally authored
native Responses frames drove these actual operations:

1. `modules.artifacts.file` imported `zinusya_files.fetch` using that opaque ID.
   The persisted artifact matched the original bytes/SHA; the Telegram journal
   recorded `getFile` for the exact uploaded source.
2. `audio_transcribe` received the actual returned handle. Its prepared intent
   referenced the original artifact and a distinct worker-generated mono16kHz
   WAV,32078 bytes. The ASR receipt's step ID, byte count and SHA matched that
   extracted artifact. Exactly one local ASR request returned
   `QA ASR consumer transcript.` and1000ms duration usage.
3. The final frame used the actual native transcript result. Its27-byte text
   artifact/handle/SHA matched that result, the task and run completed, and the
   Telegram outbox delivered the text. No profile operation, order, payment
   evidence or payment review was created.

The transcription step settled75 synthetic **microcredits** and left no run
reservation. This is local accounting, not a provider charge. External model
and ASR requests were zero; the real-model campaign remains HELD49/51.

## Preserved failures and final observation

The first fixture failed before returning its first local model response:
its `responses` method shadowed the standard HTTP status dictionary. The run's
unknown model step was archived without replay. A renamed handler passed a
socket-free regression, and a separate fresh stand performed the path above.
Earlier empty-directory ACL and cleanup short/full-container-ID guard failures
also remain recorded; they did not create a successful ASR receipt.

The fresh controller later failed only its observation comparison: the tool
encoded a UUID as compact hexadecimal while PostgreSQL returned its hyphenated
form. By then the task/run and transcription were completed. A separately
reviewed, read-only observer normalized both UUIDs and verified the same
source/extracted/transcript graph, duration, settlement, owner, worker limits,
delivery and zero business writes. It dispatched no input, model or tool.
The original controller remains FAIL; final acceptance belongs to this separate
observer of the unchanged completed execution.

The local provider was explicitly stopped with a bounded operator stop;
exit137 without OOM is recorded, not described as graceful shutdown. Both
stands' three databases were archived and fully decoded before removing only
their owned resources. A separate root inventory confirmed zero containers,
volumes and networks for both project and owner selectors. Host evidence and
original failure receipts remain protected. Full decode is not a restored-DB
exercise.

The synthetic consumer proof adds no real Telegram, paid ASR quality, real-model dialogue or production
acceptance. Existing file authorization/refetch, worker limits and completed
[model voice](shuttle-persona-model-qa-2026-10-23.md) and
[local pilot](shuttle-local-pilot-qa-2026-10-23.md) evidence retain their exact
versions and scenario scope.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
