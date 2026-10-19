# Owned primary schema59 installation, 2026-10-19

## Accepted scope

The owned shuttle-live-test primary now runs schema59 and the zinusya manifest
mvp-68 on the previously accepted Linux/amd64 image:
`sha256:68d82bd9d01e15a6c04464e36555c4e9252acd4a273f93b2f96798a2a74292f4`.
This is a local installation and recovery checkpoint. It does not certify
production readiness, actual Telegram WebView behavior, or unattended model
interpretation. No test Telegram message or new Loom run was submitted during
the upgrade. Loom still has132 runs. No developer Loom stand or production
service was changed. The separate feature QA records retain their original scope.

## Cutover and preservation

The [isolated migration and restore rehearsal](shuttle-primary-upgrade-rehearsal-qa-2026-10-19.md)
preceded this installation. Readiness checks found no active bot task, unresolved
outbox delivery or active Loom run. Business notices were terminal, including all
seven order evidence notices. No booked session was eligible for an immediate
new specialist upcoming-session notification.

The original polling container was stopped, then the bot received normal
SIGTERM through docker stop with a five-second bound. Bot shutdown took0.625s,
with exit0 and no OOM. This measures shutdown, not total upgrade downtime.
Original stopped bot/poller logs and a fresh schema49 custom-format dump were
retained before container replacement. Only the bot image changed in the resolved
Compose model; initialization parameters, secrets, mounts and peer services stayed
unchanged.

The candidate migration command ran exactly once and exited0 without OOM.
Versions50 through59 were checked against the original Goose history. All104
original-column relation projections matched the stopped schema49 database,
including receipts and queue rows. Existing sequences matched except for the
advancing Goose history sequence. Business projections were checked again after
candidate readiness, excluding live River scheduling tables. The database OID
and PostgreSQL cluster identity stayed unchanged. No Down or database replacement
was performed, and old/new bot processes never overlapped.

## Retained parser failure and recovery

The first container-creation command used compose create --no-deps. The installed
Compose version rejected that unsupported flag before replacing any container.
The controller stopped at candidate_create_pending. Inspection proved that the
original bot and poller IDs were still present and stopped; the migration had
already completed and its temporary container/environment had been cleaned.
The failure diagnostic and phase output were retained. The same checkpoint later
continued through its normal observation phases; the failed command is not
represented as a successful creation.

A separately reviewed one-shot recovery checked that exact parser failure, the
original stopped runtime, pinned inputs, schema59 and preserved projections.
It recorded intent before using the supported equivalent:
compose up --no-start --no-deps --force-recreate --no-build --pull never shuttle.
Only the bot container was recreated; the candidate was not started by recovery.
Migration was not replayed. The original controller then observed the created
candidate ID and continued through its remaining phases. The historical controller
is evidence of this installation, not a reusable deployment command.

## Startup acceptance and cleanup

The candidate passed readyz and the zinusya/mvp-68 manifest check. Its container
identity and start/restart epoch were checked again before polling resumed.
Acceptance required a fresh successful polling cycle from the original poller,
with stable identity and restart count. Observation of that same pending instance
continued without repeating its start command.

All11 primary services are running. The migration container and its copied private
environment are removed; no temporary QA service remains from this cutover.
The old image is retained for the already rehearsed schema49 restore path.
Preupgrade and post-upgrade dumps, original service logs, migration diagnostics
and the fresh polling-cycle log are retained locally. A new independent installed
runtime baseline checks configuration hashes, service identities, schema/manifest,
original domain state and bounded terminal River retention. The old schema49
baseline remains unchanged; it no longer describes the installed epoch.

A rollback after accepting new traffic must preserve that newer history. This
installation does not authorize replacing it silently with the schema49 dump.
No full old-application rollback or new end-to-end feature flow is claimed here.

## Evidence and next stage

Ignored records under .cache/shuttle-work/avatar-primary-upgrade-v1-qa/cutover-v1/
include checkpoint.json, review-gate.json, candidate-create-recovery.json,
installation-acceptance.json and installed-epoch.json. Private dumps, environments
and log content must not be published. Controller review took three passes;
verified fixes cover fresh health before ingress, cleanup, failure fencing and
fresh polling-cycle proof. Recovery and the new observer passed fresh independent
review. The Git index stayed unchanged. No commit, push or publication occurred.

Continue with the remaining infrastructure roadmap from this schema59 baseline.
The jointly deferred actual WebView/CA/zitadeltg session and recorded meal financial
and production-retention policy questions remain separate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
