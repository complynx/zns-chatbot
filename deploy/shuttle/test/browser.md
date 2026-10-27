# Owned browser ingress

This opt-in gateway prepares the local Mini App and same-origin zitadeltg routes.
It belongs only to Docker project `shuttle-live-test`. It does not install browser
trust, configure Telegram Login or prove bidirectional account linking.

The gateway uses Traefik 3.6.25, pinned by image index digest. This reuses the
Traefik family already used in `server_configs`, with a separate file-only
configuration. The upstream application is [MIT licensed](https://github.com/traefik/traefik/blob/v3.6.25/LICENSE.md);
Alpine runtime packages have their own licenses. This test image is not part of
the Zinusya production image or its dependency audit.

## Before starting

Start the existing owned app/IAM first. Keep all current compose overrides; do not
run an unqualified `up` against the base file or replace existing candidate images.
Verify the root/leaf files prepared by `tls.py` exist and that subnet
`172.29.253.0/28` does not overlap another Docker network. If it conflicts, choose
and review another subnet and matching addresses before creating the network.
The gateway uses UID1000, matching the existing leaf-key ownership. It mounts only
the leaf certificate/key and public trust bundle, never `root-ca.key`.

Check the resolved Compose model privately. `compose config` expands secrets;
do not publish its output. The browser overlay changes only the new gateway and
adds a dedicated network. It does not modify app, Loom or native IAM services.

## Preflight without interrupting IAM

Set `TEST_BROWSER_IAM_PORT=18445` for the invocation. The following uses POSIX shell
syntax; PowerShell callers should pass this environment variable to the child
process rather than paste the prefix syntax:

```sh
TEST_BROWSER_IAM_PORT=18445 docker compose \
  --project-name shuttle-live-test \
  --env-file .cache/shuttle-live-test/compose.env \
  -f deploy/shuttle/test/compose.yaml \
  -f deploy/shuttle/test/browser.compose.yaml \
  --profile browser up -d --no-deps browser-gateway
```

The existing native IAM stays on18443. The gateway publishes18445 for IAM preflight
and18444 for the app, both on127.0.0.1. IAM discovery and redirects still advertise
18443; preflight is not a canonical-origin login test. The app is served at
`https://app.zinusya.localhost:18444/app/`. DNS/routing must reach this machine;
certificate SANs alone do not establish reachability or mobile access.

Native probes may explicitly map the two test hostnames to loopback and trust the
public local CA. Report those as probe-specific trust/DNS, not browser acceptance.
A real browser must trust the root independently. Never bypass a certificate
warning to turn a failed browser check into a pass.

## Route and trust boundaries

On the IAM entrypoint, only Host `host.docker.internal` matches. `/tg` and `/tg/…`
route to `zitadeltg:8080` without stripping the prefix. `/tganything` does not
match that router. Other IAM paths remain on `https://zitadel:8080`, preserving
Login V1 callbacks and resources. Upstream TLS uses the public CA bundle and
`serverName: host.docker.internal`, because the leaf does not include `zitadel`.
Verification is enabled. Proxying upstream to the canonical public port would
create a loop and must not be substituted for the internal service address.

On the app entrypoint, only Host `app.zinusya.localhost` and `/app` or `/app/…`
match. Private agent APIs, metrics and health endpoints are not exposed there.
Forwarded headers from clients are not trusted. The standard `Forwarded` header
is removed. Each router fixes Host and forwarded host/port/protocol to its configured
canonical HTTPS origin; the original request port must not alter Zitadel issuer.
Traefik supplies the client-address forwarding metadata. There is no
Docker provider/socket, dashboard, ACME or access log containing auth queries.
Root filesystem is read-only, capabilities are dropped, privileges cannot grow,
and CPU/memory/PIDs/logs are bounded.

The optional prelink listener uses container port19443 and network alias
`identity.zinusya.localhost`; Compose publishes no host port for it. Only HTTPS
Host `identity.zinusya.localhost`, POST and exact
`/internal/identity/telegram-prelink` route to `http://shuttle:9090`. No other
private agent/admin/metrics path is proxied. The existing wildcard leaf covers
this alias. Canonical forwarding headers are fixed; Origin and Fetch Metadata
are retained so the API can reject browser-origin calls. This route does not
replace backend service-bearer/proof authentication. Any container with network
reachability still needs those credentials; this is not a container-specific ACL.

The gateway loads file configuration once (`watch: false`). Reviewed source
changes do not update the running gateway. Recreate only the owned gateway after
privately validating its complete current Compose model and capturing rollback
configuration. A brief IAM/app ingress interruption is expected; native IAM and
bot/Loom/database need no recreation for this route alone. Endpoint enablement,
schema migration and bot/provider image replacement are separate gates.

For the private prelink route and its authorization boundary, see the [prelink API contract](../../../docs/shuttle-telegram-prelink-api.md).

The dedicated browser network reserves gateway address172.29.253.2. A later
zitadeltg service must trust only the verified gateway hop, not the whole Docker
subnet. Its HTTPS callback must reach this same gateway; the network alias
`host.docker.internal` resolves to it on that dedicated network. Other existing
services retain their current DNS/network configuration. Do not mount test echo
services on `/tg` after preflight; that route is not ready until the real provider
is configured.

## Canonical IAM-port cutover and rollback

`browser-iam.compose.yaml` removes only the native IAM host publication using
Compose's `!reset` tag. Use it only after reviewed preflight, saved current
container/config/certificate identities, a database backup and a quiescent owned
queue. Preserve all current IAM environment, image, command, volumes and issuer.
Stop ingress and owned consumers as required by the reviewed execution plan.
Recreate only owned Zitadel to release18443, then start the gateway with its
canonical default18443 and app18444. Do not reinitialize IAM or delete volumes.

Afterward verify the exact old issuer, CA/leaf fingerprints, application access,
token exchange, route/header isolation and stable persisted identity. Add the real
provider separately, with its persistent signing key and proper Telegram Web Login
configuration. This overlay alone does not create an IAM IdP or an external link.

Rollback stops the owned gateway and recreates owned Zitadel with its saved
original port publication. Keep IAM/DB/CA/provider keys and original configuration.
No production or developer-owned Traefik, IAM or Loom service is involved.

## Acceptance

Validate canonical TLS routes and the original issuer independently from browser behavior. See the [Mini App contract](../../../docs/shuttle-miniapp.md).

Browser trust, real Telegram-issued initData, cookie relay and actual Telegram OIDC require interactive checks. The provider route follows the [isolated runtime instructions](zitadeltg.md) and the [explicit-link contract](../../../docs/shuttle-telegram-prelink-plan.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
