# Private prelink HTTPS route: isolated acceptance

The owned gateway source now has a dedicated identity listener on container
port19443. It is not published on the host. Compose adds the browser-network
alias `identity.zinusya.localhost`; the existing wildcard leaf/public CA are reused.
Routing uses [Traefik3.6 Host, Method and Path matchers](https://doc.traefik.io/traefik/v3.6/reference/routing-configuration/http/routing/rules-and-priority/).
Only the exact prelink POST reaches Shuttle's internal9090 listener. Public IAM
and app routers remain on their explicit entrypoints and do not expose this route.

Independent configuration/security review found no actionable findings. Native
preflight used the pinned Traefik image, current reviewed file configuration and
existing leaf/public CA on a new owned internal Docker network with no host-port
publication. A small synthetic HTTP peer represented Shuttle's8080/9090 listeners.
Neither the provider nor the bot application was started or reconfigured for this
probe. This is TLS/router acceptance, not API authentication or account linking.

Eighteen HTTP checks passed: exact POST/body forwarding to9090, canonical Host
and forwarded host/port/protocol, removal of untrusted Forwarded, preservation of
dedicated authorization and Origin/Fetch Metadata, refusal of GET/OPTIONS/PUT/DELETE,
trailing slash/child paths and unrelated private paths, wrong host, cross-entrypoint
requests, and continued app-route reachability. TLS used explicit public-root
trust with normal hostname verification, never skip-verify. The retained Origin
and fetch headers must be rejected by the real backend, not silently sanitized
into a server-authorized call. Browser trust is not established by this probe.

All10 main stand container IDs, images and start times matched before/after.
The exact labeled gateway exited0. The synthetic Python peer did not handle
SIGTERM as PID1 and was killed after its5s cleanup allowance (exit137, OOMfalse);
this is a fixture cleanup limitation, not a bot/provider shutdown result. Both
containers and their empty isolated network were removed. No new volume, published port or Telegram
message was created. Ignored evidence is under
`.cache/shuttle-work/prelink-route-qa/` including probe results, terminal states,
main container fingerprints and gateway logs. The isolated preflight was followed
by the separate owned gateway recreation described below.

The first standalone Compose-model lookup omitted the browser profile and found
no gateway service. The corrected `--profile browser config --format json`
lookup verified the network alias and absence of a19443 host publication. This
lookup failure did not affect the successful TLS probe or any running service.

## Owned main gateway application

The complete current gateway Compose model was validated privately, preserving
its pinned image, fixed browser-network IP, loopback18443/18444 publications,
read-only mounts and resource/security settings. Previous route/static files and
a rollback overlay/command were saved locally. The owned task queue was quiescent.
Only the gateway was recreated; no IAM, bot, Loom or database recreation occurred.

Six further TLS checks from a bounded container on the browser network passed:
the new identity hostname resolves via its actual Docker alias; exact prelink
POST reaches the current application and returns404 because that application
endpoint remains disabled; GET and unrelated private paths return404; Mini App
renders and IAM discovery retains the exact original issuer. The app hostname
uses a probe-only DNS mapping to the existing gateway alias while normal TLS
hostname verification remains enabled. This does not install browser DNS/trust.
The isolated synthetic-upstream checks above establish exact forwarding; a404
from the disabled live endpoint does not prove prelink authentication/linking.

All nine companion container IDs/images/start times, four certificate/trust-file
hashes and eight selected business-table hashes matched before/after. The live
route probe exited0/OOMfalse and was removed; only the ten normal stand containers
remained at this route checkpoint. No19443 host port is published. At that
checkpoint the main stand retained its previous bot image/schema32 with prelink
disabled; the later [main image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md)
preserves that disabled-endpoint boundary.

Failures are retained in ignored evidence. A preflight assertion compared a
Compose string memory limit with an integer and was corrected before recreation.
After successful recreation a compound exact config assertion failed; the same
live gateway was inspected and its pinned image, expected command/environment
and publications revalidated without recreating it again. The initial live probe
passed four identity requests before an app-host DNS connection refusal; adding
the probe-only app mapping corrected that fixture assumption. No healthy-outage
duration or exact old environment-list equality is claimed from these probes.

Subsequent [patched image/schema gates](shuttle-prelink-images-qa-2026-10-16.md),
[combined real-IAM/provider callbacks](shuttle-prelink-combined-qa-2026-10-16.md),
[concurrency and real IAM outage](shuttle-prelink-resilience-qa-2026-10-16.md), and
[main image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md) have scoped
acceptance. Main provider/backend activation remains open.
Actual Telegram Web Login proof, browser root trust and interactive login remain
separate. This result does not establish end-to-end login or production readiness.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
