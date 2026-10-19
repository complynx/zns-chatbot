# Historical registration point-read QA — 2026-10-15

Scope: one known literal key in an owned imported users record. This does not
discover legacy registration keys, migrate live accounts, confirm payments or
complete production migration. See [contract](shuttle-history-registration.md).

Architect and senior Go consultation selected an on-demand typed read through
the existing archive validator. No new schema or derived storage was added.
Independent correctness/privacy and quality reviews found no actionable source
defects; a fresh review after lint fixes also found none. The index stayed empty.

Linux Go 1.27.2 race checks passed for the full legacy-import and API packages,
followed by focused integration checks on real disposable PostgreSQL. Final
focused checks passed for embedded projection cases, historical registration API,
existing owner history and history API checks. They cover owner/foreign-admin
access, revocation, Telegram rebinding, preserved receipts and old projections,
one DB connection, cancellation, rejected final authorization, missing and
unsupported data, malformed archive bytes, source hash and membership mismatch.
Strict lint for legacy-import, DB, API and integration packages reported 0 issues.

The final test container exited 0 without OOM and was removed. Its dedicated
PostgreSQL exited 0 without OOM and was removed with its disposable volume.
The earlier combined 251-test verification predates these changes; it is not a
claim that the entire expanded integration suite has been rerun for this slice.

Owned native QA passed with real Zitadel and Loom. A full private PostgreSQL
backup preceded replacement of only the bot container. Schema stayed 31 and
manifest became mvp-31. The source inventory covers 211 production paths,
including embedded assets; before/after build hashes match. A bounded candidate
build passed.
The candidate reuses accepted certificates, avatar executable and license files.
This is not a full production Dockerfile or deployment acceptance gate.

QA helper review found missing checks for preservation of prior history rows and
source/image provenance, then an incomplete inventory of embedded inputs. Those
checks were added before native execution. Original evidence is retained locally. A fresh helper review found no actionable
issues before execution.

Native exact-approved storage/apply inserted one synthetic users history record.
All pre-existing legacy row hashes remained present and unchanged. Owner API
reads returned claim, missing and unsupported outcomes; a foreign administrator
received 404. The claim preserved explicit paid=false and excluded a private bank
canary. Actual Loom `script` dispatch called
`modules.zinusya_history.registration(...)` with a real owner token and returned
the same claim. The run completed under the manual Codex test provider. This is
real tool dispatch, not an unattended model classification evaluation.

Archive/history hashes stayed stable during reads. Live business and bot
inbox/task/outbox/conversation fingerprints stayed unchanged throughout. Loom
container identity/start time and bot/Loom config hashes were preserved. No
Telegram message was sent. No production or developer Loom stand was changed.
There was no new restart/fault-injection or whole-bot shutdown acceptance in this
slice. CA/zitadeltg interaction, real export selection and migration remain open.

The accepted owned image is
`sha256:04b16e481004a2ef6b86f9f98a8888565618f78569e5a5b7100e438df286f5ef`.
Ignored local evidence lives in `registration-read-qa/checkpoint.json`,
`registration-read-runtime-final.json`, the source/build provenance and focused
container logs. These are local QA evidence, not release or CI artifacts.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
