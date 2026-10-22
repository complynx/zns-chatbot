# Published IAM image compatibility, 2026-10-22

The exact published images observed on the server passed 35 isolated ZitadelTG
compatibility subtests. The Go test harness ran with `-race`; the published
provider executable and Zitadel were not race-instrumented. Production was read
only; neither
the primary test IAM nor the separate base IAM installation was replaced.

## Inputs and execution

- Zitadel 4.20.0: `ghcr.io/zitadel/zitadel@sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`.
- Published provider: `complynx/zitadeltg@sha256:fa6cc200c8ff0b6dda717ebe8194ab92092ee18a5a4005d26f426aa04658736b`.
- Upstream suite source: `14e11cb3c8a2e03b629bb737705c1201e95d47f7`, exported from
  the sibling repository without changing that checkout. Each exported file was
  hashed. No upstream source was added as a tracked Zinusya dependency.
- PostgreSQL 17.9: upstream's pinned `pgvector/pgvector` image, fresh tmpfs data.

The test image copied `/usr/local/bin/zitadeltg` from the published provider image;
it did not substitute a locally rebuilt provider. The harness used the upstream
Go 1.26.3 toolchain, pinned by digest, with bounded Go concurrency. This does not
change the bot's Go 1.27 requirement. Its test command was:

```text
go test -race -tags=integration -count=1 -timeout=12m -parallel=2 -v ./integration
```

The owned `zinusya-iam420-compat-v1` project used fresh certificates, keys, tenant,
OIDC client and synthetic users. No host ports or production credentials were
used. Memory/CPU/PID limits were explicit. HTTPS verification remained enabled.
Only Telegram's signing authority was replaced with a controlled JWKS/token
fixture; the provider executable, HTTP relay and Zitadel were real.

## Accepted behavior

All 35 subtests passed, including manual registration and returning users,
automatic creation, verified synthetic email, phone and single-name handling,
email linking to an existing user, manual linking, disabled linking and profile
updates enabled/disabled. Identity stayed stable after username changes and
identical Telegram subjects remained isolated across bots.

Successful flows obtained an authorization code, exchanged it with PKCE,
verified the ID token and checked the subject through `userinfo`. Negative cases
covered invalid Telegram claims/signature, state replay/browser binding,
missing IAM cookies, wrong relay issuer/header/audience, MFA/email-verification
requirements and rate limits. Process restart preserved the signing key and a
fresh authentication succeeded. These tests do not prove an unconfirmed pending
browser state survives a provider restart; the upstream contract discards it.

## Retained failures and cleanup

The first controller attempt failed Compose parsing because tmpfs lists merged
into duplicate mounts. No resources were created. An explicit `!override` fixed
that input. The next attempt built the fixture successfully but rejected the
unsupported `compose run --no-build` flag before creating test containers. The
accepted test reused the built immutable fixture image and corrected CLI flags.

The suite exited 0. Its controller then failed a cleanup postcondition because
Compose retained the stopped one-off suite container and its bootstrap volume.
Full container logs and inspect records had already been exported. A separately
reviewed cleanup removed that exact owner-verified container by full ID, then its
owned volume. No test or migration was repeated. Final readback confirmed no
project containers, networks or volumes remained; all 11 original running
container IDs were retained and running without OOM. No test container reported
OOM before removal.

Original failures, logs, source hashes and final `acceptance.json` remain under
`.cache/shuttle-work/iam420-compat-20261022-v1/`. Each controller/fix received an
independent review before execution. The final cleanup review had no actionable
findings. Zero real Telegram authorizations and zero model requests occurred.

## Boundaries and next gate

This proves the published provider's tested Login V1 behavior against fresh
Zitadel 4.20.0. It does not prove real Telegram JavaScript/consent, Login V2,
production readiness, an upgrade of an existing IAM database, or Zinusya's own
prelink callback, impersonation/token-refresh and profile-sync paths on 4.20.0.
The primary and separate base IAM still use the previously tested 4.16.3 image;
the base provider still uses its previous test image. Qualify the Zinusya-specific
paths in a separate owned fixture before replacing those installations.

## Zinusya-specific follow-up

The separate `iam420-zinusya-v1` mock-Telegram fixture subsequently passed
Mini App bootstrap and profile acceptance with the same pinned Zitadel 4.20.0
image. It used the current bot image built from `74604b7e`, schema 72 and
manifest `mvp-92`. The mock stand factory accepts an optional, locally resolved
`--zitadel-image`; omitted selection preserves its previous pinned default.
Saved stand configuration wins over an ambient `QA_ZITADEL_IMAGE` value. Actual
Compose resolution checked both the old default and the new pinned selection
under a deliberately conflicting ambient value. Factory tests passed: 29 tests,
including two skips.

The first bootstrap failed with HTTP 401. The fixture's exchange client had
creation events but was absent from the `apps7` projection; its management read
returned 404 and token acquisition reported an invalid client. Recorded event
positions and creation timestamps moved backwards by about 50 ms between
sequential application-creation requests. This is evidence of an ordering
problem in this fixture, not proof that every 4.20 installation is affected or
of the clock change's cause.

Only this synthetic fixture was repaired. With IAM stopped, a full custom-format
database backup was saved and decoded, and the four derived `apps7` tables and
their projection cursor were reset in one transaction. The original exchange
application and client IDs then became readable and active. The repair controller
failed its final whole-eventstore hash assertion after restarting IAM. A separate
read-only comparison against the backup confirmed all 1,022 original event rows
were byte-for-byte preserved; the 14 added rows were startup migration events
(`system.migration.started` and `system.migration.repeatable.done`). The original
failure receipts remain intact. This reset is a recorded fixture recovery, not
an automatic application recovery mechanism or an upgrade procedure.

A separately reviewed continuation reused the original synthetic Telegram user
and original OIDC client. It verified:

- Signed Mini App bootstrap and profile reads through the bot's real
  user-token acquisition and verification path.
- Two compound-name edits, each acknowledged by the durable worker and read
  back from the real IAM profile; unrelated IAM profile fields stayed unchanged.
- Replay of the first operation returned its historical receipt without
  replacing the second name.
- Repeated bootstrap reused the same local identity; the second session and
  profile survived a graceful bot restart. Bot stop took 0.547 seconds.
- No Loom runs or bot tasks were created. No real Telegram authorization or
  paid-model dispatch was performed by the controller.

The follow-up passed and all nine fixture containers stopped with exit 0 and
without OOM. Evidence remains under
`.cache/shuttle-work/iam420-zinusya-20261022-v1/`, including the failed
`profile.json` and `projection-repair.json`, the event comparison
`projection-repair-observation.json`, and successful
`profile-continuation.json`. The continuation received an independent review
with no actionable findings before execution.

This adds bot-specific Mini App, token acquisition and profile-sync evidence on
a fresh, repaired 4.20 fixture. It does not cover an existing IAM database
upgrade, token expiry/refresh, the Zinusya prelink callback, or real Telegram
consent. The primary and separate base IAM installations remain unchanged.

A later [existing-database copy rehearsal](shuttle-iam-existing-upgrade-qa-2026-10-22.md)
also passed 4.16.3 to 4.20.0 migration, preservation of original events and
user/grant records, old/fresh user-token checks, a 4.20 restart, and backup-restore
authentication on 4.16.3. That isolated rehearsal did not upgrade the primary or
base installation and did not run the bot or provider.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
