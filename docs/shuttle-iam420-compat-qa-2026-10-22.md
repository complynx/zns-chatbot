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

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
