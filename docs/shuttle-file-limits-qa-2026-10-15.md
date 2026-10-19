# Attachment read limit acceptance — 2026-10-15

The owned local stand accepted one synthetic 21 MiB text document from the
authorized Telegram Web account in @complynx_test_bot. Real local Zitadel,
Zinusya and Loom were used, with a manual scenario provider. This checks file
transport and tool behavior, not unattended model interpretation.

## Failure and correction

The document size was 22,020,096 bytes. Intake persisted its opaque reference and
metadata. The first explicit Loom artifact fetch returned `module HTTP status 503`;
no artifact was imported. That original task completed with a truthful explanation
of the failure. The upstream Telegram response body was not captured; this record
does not claim a particular upstream error string or status.

The shared API reader now checks the persisted Telegram size after authorization.
Known sizes above 20 MiB return HTTP 413 before Telegram IO. This applies to fetch
and content routes, including consumers of the shared reader. Metadata stays
readable by the owner. Unknown and exact-limit sizes retain existing remote
metadata and body limits. The Telegram client was not changed.

Only the owned bot container was replaced. Schema 31 and manifest mvp-30 remained.
A separate signed Telegram request then read the same opaque reference and made
one Loom fetch attempt. It returned `module HTTP status 413`, with no artifact.
The file was uploaded once in total. Both tasks completed and selected 27
business/configuration/audit hashes remained unchanged. Runtime task, conversation,
inbox, outbox and identity state are outside that preservation comparison.

## Verification

- The new behavioral test failed before the fix: expected 413, received 503.
- Focused Linux/PostgreSQL race checks passed: integration 19.493 seconds,
  Telegram 1.042 seconds. These included intake/restart, archive/file and avatar
  paths selected by the saved command, plus the new file-limit cases.
- Final new behavior checks passed under race in 2.601 seconds after formatting
  and the lint correction. Owner fetch/content return 413 without getFile calls;
  foreign and revoked identities retain 404/403. Unknown and exact-limit declared
  sizes continue through the reader with a small actual body.
- Two independent read-only reviews found no actionable findings. Strict lint
  for API, Telegram and integration packages reported zero issues after replacing
  one integer formatting call in the test. No new full-suite pass is claimed.

The local reader ceiling is 20 MiB. Telegram documents a cloud download limit of
20 MB in its [getFile contract](https://core.telegram.org/bots/api#getfile). These
are separate limits. This acceptance does not establish behavior for unknown-size
cloud rejections, every byte boundary of the cloud limit, production readiness,
or unattended media classification. Those remain separate checks.

## Evidence

Ignored checkpoint: `.cache/shuttle-live-test/oversize-file-qa/checkpoint.json`.
Source freeze: `.cache/shuttle-work/file-limit-source-freeze.json` (321 paths).

- Original task: `f81c41c8-9930-49b8-b5bd-dabdeb5e79a0`.
- Original run: `743b6a6b-5dc5-44c7-80eb-badca552767c`.
- Opaque file: `c818d770-29a8-4d50-99f7-d487d8ccf6af`.
- Fixed-case task: `ca9d2ff3-8f20-4a2d-bad1-9dd3f504dc5b`.
- Fixed-case run: `2f18f144-c5e8-4886-b387-e28065a7c6de`.
- Candidate: `sha256:00f6c2c92620647f9f04b66318dc0220421eb3e70fd22d4ef8160d3b386a7566`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
