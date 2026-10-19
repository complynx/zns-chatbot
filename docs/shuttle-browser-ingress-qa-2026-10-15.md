# Owned HTTPS browser ingress QA, 2026-10-15

## Result and scope

Native preflight and canonical IAM-port cutover passed on the disposable
`shuttle-live-test` project. This accepts gateway routing, explicit local-CA trust
in the probe, Mini App session operations with synthetic signed proof and fresh
real IAM/Loom authorization. It does not accept real Telegram OIDC, bidirectional
linking, browser-installed CA trust or Telegram WebView behavior.

The opt-in configuration and rollback procedure are in
[the gateway runbook](../deploy/shuttle/test/browser.md). Traefik3.6.25 is pinned
by image index digest. The gateway has no Docker socket/provider, dashboard,
ACME resolver or request access log. It runs with a read-only root, UID1000,
dropped capabilities and bounded memory, CPU, PIDs and rotated service logs.
Only the leaf key/certificate and public trust bundle are mounted; the root CA
private key is excluded.

## Native preflight

The first preflight retained the request's temporary port18445 in Host. Zitadel
therefore returned an incorrect discovery issuer ending in18445. No Mini App
bootstrap was dispatched in that failed exercise. The probe also expected an
absolute asset path although the existing page correctly uses relative assets.
Both failures remain recorded in the original checkpoint.

The correction fixes Host and forwarded host/port/protocol to each backend's
canonical HTTPS origin and removes the client-supplied Forwarded header. The
next exercise passed:

- Discovery advertised the original issuer, `https://host.docker.internal:18443`,
  while the preflight request entered on18445.
- `/app/` and its relative assets were served on the app origin. Private API,
  metrics/health paths and the wrong Host returned404.
- `/tg` routing preserved its prefix; `/tganything` reached the IAM fallback.
  A temporary header peer verified the canonical origin and rejection of forged
  forwarding metadata, then was stopped and removed.
- A client without explicit local trust rejected the CA. The probe with the
  public local CA and probe-specific loopback DNS mapping passed TLS verification.
- A locally signed synthetic Telegram Mini App proof completed bootstrap,
  owner account read and logout. Wrong Origin was rejected, and the consumed
  session was unusable after logout. This was not Telegram-issued initData.

## Canonical cutover

Before mutation, the runner verified the candidate IAM model changes only host
publication, saved a PostgreSQL-format IAM backup and configuration/certificate
hashes, and required no unfinished bot task. It stopped only the owned Telegram
poller, recreated owned IAM without its host port and recreated the gateway on
canonical18443 plus app18444. IAM image, environment, command, mounts, database,
master key and issuer were preserved.

The runner then obtained a fresh IAM token for the existing account, exercised
its private owner-profile API and created a real Loom authentication session.
The existing poller was restarted and completed a fresh polling cycle. No test
Telegram message was sent by this cutover.

Ten selected business-table hashes and task/inbox/outbox/user counts matched
before and after. App, Loom and PostgreSQL container IDs and start times matched;
root, leaf, leaf-key and trust-bundle hashes matched. Independent post-cutover
inspection confirmed those container identities, native IAM's absent host port,
gateway loopback publications and running/non-OOM state. This is bounded native
acceptance, not a worst-case restart SLA or production release.

## Evidence and remaining gates

Ignored local evidence is retained under
`.cache/shuttle-live-test/browser-gateway-qa/`: preflight `checkpoint.json`,
`cutover.json` with stage `ready` and `passed: true`, IAM backup, upstream license
and provenance, and the pre-correction route snapshot. Never publish that backup
or private runner inputs. The original failed preflight is preserved alongside
successful corrected observations.

The stand now has ten owned containers. The header peer is absent. There is no
running zitadeltg service or login-enabled external provider yet; `/tg` cannot perform
login. A subsequent [owned Linux runtime probe](../deploy/shuttle/test/zitadeltg.md)
passed routing, public-key persistence and restart checks with a synthetic Web
Login secret, then removed its provider container while preserving the signing
volume. Next comes the trusted prelink contract, followed
by real bot-first/provider-first identity acceptance. Browser trust/DNS, actual
Telegram OIDC credentials and interactive cookie relay remain separate gates.
No developer-owned Loom stand or production service was changed. No commit,
push or publication was made.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
