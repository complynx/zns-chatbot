# Loom upstream consumer acceptance, 2026-10-15

The owned Zinusya test stand now uses committed, unmodified Loom code for both
operation sealing and PDF rendering. This supersedes the earlier working-tree
seal fixture and failed PDF visual checkpoint. Those historical results remain
unchanged. This is bounded consumer acceptance, not production readiness.

## Provenance and deployment

Upstream confirmed the JSONB sealing defect in
[issue 3](https://github.com/complynx/loom/issues/3#issuecomment-6145879126)
and the missing PDF fallback fonts in
[issue 4](https://github.com/complynx/loom/issues/4#issuecomment-6156237391).
An immutable archive of commit `1050d74eba2e2775c2196035a2c2611a4ece39f5`
includes sealing fix `41daf549dd9b57f27b830f0b9c93cfceb58bf9ba`.
Its tree is `3dae5423b40131303b84b85aeaf92302d26da2a9`; archive SHA256 is
`c3ce86ac24854117940748641af774e6d504eda16158394655d00fc7f23c830e`.
No sibling checkout, index, Git configuration or developer stand changed.

The upstream Dockerfile built both images without source changes:

- API: `sha256:834dc24f01038bebac0811fc6eb43d039dfbd9188c3dadda93627e108a275471`.
- Worker: `sha256:0a80b34f827cc7db5900f5c6662f62fa83dc8c8073dfd69e3f5fb631b31beb83`.

The worker build passed its locked package/source/checksum gate and includes the
matching source and notices. Roboto and Roboto Mono provide bounded fallback
fonts; Helvetica resolves to Roboto Regular in this image. This does not provide
exact original font metrics or complete CJK/emoji coverage. The separate media
image retains its upstream Poppler/FFmpeg distribution obligations.

A dedicated builder used 1.5 GiB memory, no swap, two CPUs and one concurrent
build step. Both builds exited successfully without an observed OOM. The builder
and its private cache were removed afterward. Runtime worker inspection confirmed
UID/GID 10001, no network, read-only root, all capabilities dropped, 512 MiB
memory with no swap and bounded temporary storage.

Verified PostgreSQL-format backups of both owned databases preceded rollout.
Loom migrations did not differ from the old snapshot; no migration was needed.
Zinusya remains on schema 26 / manifest `mvp-26`. Its application image is unchanged.

## Operation sealing

Real Zitadel credentials and native Loom prepared a nested synthetic massage
schedule under exact administrator confirmation. The schedule did not exist
before approval. Loom restarted while waiting for approval; the persisted
operation ID, arguments and approval survived unchanged. Approval then executed
successfully through the real Zinusya broker.

Repeating the same approval input with its original idempotency key preserved
schedule version 1 and exactly one operation receipt. The Zinusya status endpoint
returned that original receipt. The run completed.

Run: `e53b2a4d-2740-467b-8a6f-bc445540121a`.
Operation: `5caf445b-24af-4b17-a165-e685ea06643a`.
The separate synthetic event/schedule remains as a local QA fixture; it has no
registrations, reservations or money. Older failed operations were not resealed,
retried or rewritten as successful.

This verifies the consumer's nested-object JSONB path. Exponent and negative-zero
normalization remain outside this fix: use ordinary decimal/integer forms without
negative zero. Unknown or dispatching writes still require reconciliation.

## PDF processing

The existing synthetic 670-byte PDF from the authorized Telegram account/chat
was fetched again by its owner-scoped opaque Zinusya file ID. No attachment bytes
were sent at run creation. Native Loom imported the source and invoked its
isolated worker for bounded text extraction and page rendering.

The text contained the synthetic-document banner and amount. The authorized
PNG download was 566 by 800 pixels with 3,016 dark pixels; visual inspection
confirmed readable banner and amount, rather than a white page. The PNG SHA256 is
`dc979de3ad4276cecf145b8a8dd17beb7f426a69e590a043a8abefd039398d06`.
Run: `e02d285a-0f4b-4977-9773-fcd8fe486d23`.

This pass covers this standard-font document. Blank-page and missing-font failure
boundaries were reported by upstream tests, not repeated as consumer scenarios
here. Audio/video, OCR, scanned-document interpretation and an unattended model's
choice of file action remain separate gates.

## Review and evidence limits

The acceptance helper received independent reviews. One uncertain-event-retry
finding was fixed before execution; a fresh review found no actionable findings.
An initial rollout attempt stopped at Compose interpolation because the optional
worker image variable was absent. No service stopped. The existing backups and
checkpoint were retained; the variable was supplied and the same rollout resumed
after checking all prior image IDs. This was a harness error, not a product bug.

The scenario provider was the manual Codex mailbox. IAM, databases, approvals,
broker, Telegram source download and native media processing were real. No new
Telegram message was sent. Payment evidence/reviews, passes, pairs, profiles,
event context and the Telegram outbox retained their original hashes. No
production or browser-trust change occurred. GitHub issue closure is not claimed.

Ignored evidence under `.cache/shuttle-live-test`: upstream archive provenance,
`native-upstream-consumer-checkpoint.json`, `native-upstream-pdf-result.json`,
`native-upstream-pdf-render.png`, both database backups and the immutable-image
Compose override. Build logs/state and review snapshots are retained separately.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
