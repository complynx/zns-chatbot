# Go Linux deployment candidate

This is a reviewable deployment artifact, not a release or deployment approval.
Run nothing below against production until the frozen full-Go candidate passes
fresh independent Code QA and Telegram-like Functional Senior QA on Linux Docker,
including real isolated Zitadel. The root publication workflow and existing
`server_configs/configs/zns-chatbot` Python deployment are unchanged.

## Composition and release inputs

`platform/deploy/compose.yaml` runs one combined `zns app`, PostgreSQL 17 and
isolated CPU-only script, media and sticker helpers. There is no fake service,
fixture seeding, remote model service, GPU device or Docker socket mount. The
application uses direct OpenAI; transcription uses the existing media broker.
Sobek evaluation and native media/sticker decoding have no network. The media
broker uses a separate `zns_meter` database credential for paid-operation accounting;
the sticker broker has no database credential. The script socket is shared read-only with app via group
10002. Decoder sockets use bounded tmpfs volumes; only PostgreSQL persists data.

Reuse these existing build definitions to prepare the reviewed release elsewhere:

| Release input | Existing build definition |
| --- | --- |
| `ZNS_APP_IMAGE` | `platform/Dockerfile`, final stage |
| `ZNS_POSTGRES_IMAGE` | PostgreSQL 17 Alpine, reviewed patch and digest |
| `ZNS_SCRIPT_IMAGE` | `platform/Dockerfile.script`, runtime target |
| `ZNS_MEDIA_DECODER_IMAGE` | `platform/Dockerfile.media` |
| `ZNS_MEDIA_BROKER_IMAGE` | `platform/Dockerfile.media-broker` |
| `ZNS_STICKER_DECODER_IMAGE` | `platform/Dockerfile.sticker-worker`, with native image context from `compose.sticker.yaml` |
| `ZNS_STICKER_BROKER_IMAGE` | `platform/Dockerfile.sticker-broker` |

Copy `images.env.example` to a protected operator location. Every image reference
must have an explicit release tag and immutable digest:
`registry/repository:release-tag@sha256:<64 hex digits>`. Record the source commit,
build platform, resolved base images and review evidence with that release.
Compose requires values but does not validate their digest syntax. No registry
digest is invented here: selecting, building, publishing and accepting the actual
release images remains an explicit integration step. Some existing Dockerfiles
use mutable base tags; lock and record those resolutions before release.

Compose 2.30 or newer is needed for raw env files and merge tags. Script/media
services extend the existing Compose definitions with builds removed. Sticker
services retain existing worker images and isolation settings explicitly because
loading its source Compose also interpolates its unrelated mandatory broker
secret. No new evaluator or worker implementation is introduced.

## Secrets and database ownership

Create the absolute secret directory outside the checkout, owned by the operator
with mode 0700. Populate `app.env`, `migrator.env`, `media.env`, `sticker.env` from
the matching examples and use mode 0600. Raw env files do not interpolate `$` or
remove quotes; enter literal values without surrounding quotes. Compose injects
these as process environment because the binaries do not support `_FILE` inputs.
Anyone with Docker administration can read them; do not publish `compose config`
output, container inspection or resolved environment artifacts.

Create four independently generated password files without a trailing newline:
`postgres.password`, `migrator.password`, `runtime.password`, and
`accounting.password`. The PostgreSQL
entrypoint reads these on **first initialization of an empty volume only**.
Updating files alone does not rotate existing database credentials.

Set the app URL to `postgres://zns_runtime:<encoded-password>@postgres/zns?sslmode=disable`
and migrator URL to `postgres://zns_migrator:<encoded-password>@postgres/zns?sslmode=disable`,
using the corresponding file's password. This assumes a single trusted host and
private Docker bridge. Off-host/database-untrusted transport requires a separately
reviewed TLS configuration. The bootstrap owner has no application credential
mount; only PostgreSQL receives its password. Migrator owns `core`, `bot`, and its
public migration ledger. Runtime receives schema usage and table DML/sequence
permissions through migrator default privileges; it cannot create schema objects,
change migrations or assume the owner role. Verify these assertions on real PG
after each schema change. Existing/restored volumes require an audited role/grant
reconciliation; the initialization script is not a restore/upgrade migration.

Set `DATABASE_URL` in `media.env` to
`postgres://zns_meter:<encoded-accounting-password>@postgres/zns?sslmode=disable`,
using the password from `accounting.password`. This role receives the explicit
accounting grants installed by the credit migrations, including the administrator
projection used for credit policy. It receives no general bot/history DML or
schema-creation privileges. Do not substitute runtime or migrator credentials.
Keep `ZNS_CREDITS__ENFORCE` in `media.env` equal to the app's `credits.enforce`;
disabled enforcement still records usage. Verify both policy and ledger operations
with this role after applying migrations.

Use separate generated credentials for signing, workers and identity applications.
Signing key must be at least 32 bytes; production's other secret checks require
at least 16 bytes and reject fixture/placeholder markers. These checks do not
prove issuance or entropy. Bot token, configured numeric bot ID and Telegram
`getMe` identity must match. The media/sticker secrets in app must match their
broker env files. ASR and app keys must be issued for their intended provider.

Set the HTTPS Zitadel issuer, audience, separate bot/API applications and actor
credentials according to [identity configuration](zitadel-identity.md). The
combined app also enables [first-contact provisioning](identity-provisioning.md):
supply a separate organization-scoped management client, organization ID and
reserved `.invalid` email domain. Do not reuse the impersonation client or grant
IAM administrator rights. Trusted legacy links and live authorization convergence
still need reconciliation and acceptance; startup is not proof of identity parity.
Set the active event explicitly to the reconciled production event identifier.

Optional live sources, lineup CSV and legacy browser origins use the existing
typed configuration documented in `platform/internal/config`. Configure required
source credentials/read-only file mounts separately and test refresh failures;
the minimal artifact does not silently import Python Google credentials or event
configuration. Do not copy fixture defaults to fill missing operator data.

## Network and reverse proxy

PostgreSQL has no published port and is on an internal network. The helper network
is internal; app and media broker additionally have an egress network for their
real providers. Docker bridges alone do not provide an outbound domain allowlist;
apply host/network policy if required. Script and decoder containers have
`network_mode: none`. Only app publishes `127.0.0.1:8180` by default.

Terminate TLS at the existing trusted reverse proxy. Forward the public `/bot/`
prefix to the loopback app port with `/bot` stripped, preserving request methods,
bodies and query strings. Set `ZNS_TELEGRAM__WEB_APP_URL` to the actual public
`https://<host>/bot/miniapp/`. Do not route internal broker or PostgreSQL ports.
For a containerized Traefik proxy, loopback refers to that container; add a reviewed
private proxy network attachment to app and route its port 8080 there instead.
The old Python router must be stopped/disabled at cutover to avoid duplicate
routes. This artifact deliberately does not join an existing production proxy
network or edit its labels. Verify authenticated Mini App requests and legacy
links through the exact final public path in EN and RU before acceptance.

## Stopped-writer cutover and rollback

The following are operator steps after separate authorization, not commands run
while preparing this artifact. Use an absolute `--env-file` path and keep the
Compose project name stable so the persistent volume is not accidentally replaced.

1. Freeze release digests and the accepted configuration. Test backup restoration,
   role separation, restarts, script/audio/video/sticker flows, both locales and
   identity provisioning on an isolated Linux stand. `config --quiet` proves only
   Compose structure. Starting app makes real Telegram/Zitadel/provider calls.
2. Stop every Python writer, polling instance, job and legacy admin write path;
   verify they cannot restart. Keep Go app stopped. Capture immutable MongoDB,
   relevant media/files and configuration backups with exact cutover timestamps.
3. Initialize the isolated target with `docker compose --env-file /path/images.env
   -f platform/deploy/compose.yaml up -d postgres`. Run schema-only migration with
   the same options and `run --rm migrate`. The migrator is an explicit maintenance
   command, never an app startup dependency. No fixtures run.
4. Execute the separately reviewed disposable importer against the frozen source;
   this artifact does not include importer credentials or install it in runtime.
   Reconcile identities, balances/payments, orders, bookings, historical links,
   media, active event and pending work. Resolve rejected records and delivery
   uncertainty before admitting writes. Restore/import ownership must match the
   migrator/runtime grants. Back up reconciled PostgreSQL before starting app.
5. Keep exactly one application/poller instance. Start with the same options and
   `--profile runtime up -d`. Check readiness, expected bot identity, all helper
   operations, current authorization and restart continuity before switching the
   public proxy. Readiness alone does not prove helper or user-flow health.
6. Retain immutable source snapshots and pre-cutover images/configuration. Never
   use `down -v` for rollback or cleanup of this stack.

Before any Go writes or external deliveries, rollback can stop Go and restore the
unchanged Python deployment/source snapshot after verifying no overlapping poller.
After Go writes/deliveries, restarting Python is **not** a safe rollback: freeze
all writers, preserve PG and provider/delivery evidence, reconcile or implement an
explicit reverse migration, and obtain a decision on authoritative data. Forward
schema migrations have no automatic down path. An older Go image may run only if
its schema/data compatibility is independently proven; otherwise restore a tested
backup with an explicit accounting of lost writes and already-sent messages.

## Acceptance boundary

The candidate needs fresh Code QA and source-blind Telegram-like Functional QA on
the final Linux composition. Required evidence includes real PG permissions and
migration replay, persistence/recovery, first-contact identity behavior, exact
reverse-proxy paths, authenticated helpers, non-overlapping polling, graceful
stop/crash/restart and reconciled stopped-writer import. CPU limits are initial
bounds, not a capacity claim. Release digests, proxy attachment, operator secrets
and production data approval remain unfilled external inputs. None is accepted
merely because the configuration parses.
