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
Real Telegram Mini App login and clean provider OIDC are now accepted separately
(see [real Telegram QA](shuttle-real-telegram-qa-2026-10-23.md)); real Telegram
voice ingress/delivery and the main real-API walkthrough remain pending.

## Durable full transcript and no-charge reuse: accepted (Loom36)

Daniel deployed the updated Loom before this bounded local gate. Selected
read-only production evidence records all four services at revision
6b04ab9c050ce384057be78c7eaefd8099e03bc7, API healthy/zero restarts, and additive
schema006/audio_transcripts present with matching columns. No transcript content
was read in production. Comparison with reviewed source
6adc0dd6b16321dc73a350e2de032e83deabd9d5 contains no changed files.

N3 uses exact deployed immutable RepoDigests locally:

- API/sandbox/proxy: complynx/loom@sha256:e7a8cac6f54494b4d131fb21548e001070dda13becd45fc0759ccb251bd99314.
- Worker: complynx/loom@sha256:0f74a6d64261d1a1bdd2dd11ce571ecb2dc8bde8de0582d0423b431db1344fd5.

Docker Desktop reports manifest digests as local image IDs while the server
reports image config IDs. These observations are joined by exact RepoDigests,
not an assertion that different engines' IDs are equal.

Original N3 flow8438 terminated exit0. The complete synthetic transcript is56021
UTF-8 bytes over7 pages, SHA256
9bbc62fe7012cb3bed3a0ac6a0fbf0c7589a26514ea0276948d7003d663e6c1c.
The joined full text has the same SHA initially and after actual source/WAV blob
collection, a stop/start of the same API container, and reuse by a distinct
completed run. Both initial and post-restart reads return all7 pages. Reuse
retains original provenance and performs no source refetch/file processing.

The gate observes exactly one local ASR stub dispatch, one reservation and one
settlement; the later reuse adds no provider dispatch or charge. A staged current
human-authority revocation yields typed waiting_auth on a third run, no next
model request, no transcript disclosure and no additional ASR/refetch/ledger
effect. No order, payment evidence or review was created. The20 native Responses
frames are CONTROLLED fixtures, external model/ASR calls0 and business writes0.
This is not autonomous model discovery or another real paid transcription.

Fresh independent final evidence review is CLEAN. Unchanged developer tests for
concurrency, unknown outcomes, model upgrade and legacy paths are reused within
their original scopes; this consumer gate does not rerun the whole matrix.
[Loom issue36](https://github.com/complynx/loom/issues/36#issuecomment-6371973515)
is closed for this storage/reuse requirement, not for whole-production readiness.

N1's fixture HTTP collision failed before ASR; its three DB archives were fully
decoded and project/owner resources removed. N2 failed an image-ID preflight
before private setup with zero effects. Both original FAIL checkpoints remain,
without replay. N3's final evidence review preceded cleanup; subsequent original
cleanup20160 terminated exit0, archived and fully decoded all3 DBs. Root's separate
inventory verifies project and owner containers/networks/volumes all0 and original
checkpoints unchanged. Full decode does not claim a restored-database exercise.

Sanitized evidence: .cache/shuttle-work/direct-production-20261023/
deployed-fixes-local-identity.root.json and loom36-consumer-qa-r3/
{acceptance,runtime-observation.root,final-evidence-review,root-cleanup-verification}.json.
The original real-provider allowance remains exhausted1/1 with no retry. Real
Telegram voice ingress/delivery and Daniel's main real-API walkthrough remain
next; this gate neither spends another paid request nor authorizes production.

## Earlier consumer baseline: exact versions and path

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
