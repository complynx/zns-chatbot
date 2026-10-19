# Local Mini App launch QA: 2026-10-16

## Scope and installed state

The owned `shuttle-live-test` bot now runs schema39 / manifest `mvp-39`, image
`sha256:6899831aeb370ab40fdc392720929f449b1fd1860ed9bd0745fac4450c450616`.
Loom remains schema4 and its container was not changed. No production configuration,
commit, push or publication occurred. The existing registration-details key was
retained. Only the bot image and three startup settings changed:

```yaml
mini_app_origin: "http://localhost:18382"
mini_app_launch_url: "https://complynx.net/testbot/18382/app/"
mini_app_allow_local_http: true
```

The [configuration contract](shuttle-miniapp.md#local-telegram-launch-redirect)
explains validation and environment overrides. The HTTPS launch address is distinct
from the origin trusted for local requests. No CORS exception was added.

## Developer and activation proof

The broad Linux race gate passed 118 root tests / 205 pass events. After the final
configuration-only helper adjustment, the focused Linux race gate passed 18 root
tests / 72 pass events; this is not a second full broad run. The 838 final frozen
inputs were unchanged. Lint reported zero issues. Fresh complete source review,
documentation review and activation-script review found no actionable findings.
Earlier fixture, helper-inventory and lint failures remain in separate local
records; they are not counted as accepted runs.

The full release Dockerfile built in a dedicated builder limited to 2 GiB, no
swap, two CPUs and one parallel build job. That builder and its cache were removed.
An isolated dump of the owned schema38 database migrated to39 with the exact new
image. Every existing table retained its data; the two new review tables were
empty. Clone and migrator containers were removed.

For activation, ingress was stopped and pending work drained. The old bot exited0
without OOM. A fresh backup preceded migration. Existing table data and queue
metadata were verified, allowing only expected River startup bookkeeping. The same
poller was restored after real-IAM profile, registration-status and orders reads.
Eight companion services retained their container fingerprints. Activation evidence
is in the ignored `miniapp-local-launch-main-cutover` directory.

## Actual browser observations

Only the authorized account8968148043 and private test-bot chat6087685431 were used.
Actual `/orders` messages271/272 produced the new launch button. Telegram Web
requested confirmation and opened a Mini App dialog. Its iframe targeted the
configured HTTPS path with a Telegram-generated fragment, but stayed at its loader
with an `about:blank` child. No browser permission prompt or useful console error
was exposed. **Embedded WebView acceptance did not pass.**

Direct navigation through the public redirect reached the local page. A separate
browser tab then navigated to the exact iframe launch URL, without recording or
printing its fragment. The server accepted real Telegram-issued initData, obtained
server-side IAM authorization and showed `Signed in`. This replaces no synthetic
proof fixture and proves neither the embedded iframe nor an unattended model run.

The HTML form loaded the existing synthetic saved selection at version2/quantity3.
Saving quantity5 produced version3. Reload retained the session; selecting the same
event showed version3/quantity5. Restoring quantity3 produced version4. These were
draft changes only; no order, reservation or payment was submitted. Sign-out worked,
and reload required reopening from Telegram instead of restoring access.

Public-to-loopback restrictions are one possible cause of the iframe behavior.
Chrome documents explicit permission delegation for cross-origin local requests
in its [Local Network Access guide](https://github.com/GoogleChrome/modern-web-guidance/blob/main/skills/modern-web-guidance/guides/security/local-network-access.md).
The observed iframe did not delegate local/loopback network permissions. This is
a hypothesis, not a confirmed browser diagnosis. No browser protection was disabled,
iframe policy patched or server preflight workaround added.

## Follow-up: embedded Telegram Web opening passes

After Daniel approved the browser permission request, the existing message272
launch button opened the app inside the Telegram Web Mini App dialog. The page
showed `Signed in`, the profile, protected registration-details status and account
registrations. No separate top-level tab or copied initData was used in this check.
Selecting the synthetic saved-selection event loaded version4 with quantity3.
The current-price button returned37.02 BYN and Europe/Minsk local time inside
that iframe. A follow-up write saved quantity4 at version5; reloading the saved
selection retained4. Restoring quantity3 produced version6. Sign-out removed the
protected forms and reopening the Telegram button signed in again. These were
synthetic draft changes only; no order, reservation or payment was created.

This closes the observed embedded loading blocker and proves authorized reads
calculation, draft writes and logout/re-entry in the real Telegram Web iframe.
It does not yet repeat mixed agent/form workflows or payment-review writes. The original failed
observation above is retained. The permission approval was performed by Daniel;
no browser security protection was disabled by the agent.

## Remaining acceptance

The later [payment-review four-path check](shuttle-payment-review-real-qa-2026-10-16.md)
now verifies the reviewer HTML interface and actual embedded writes with a manual
provider. Mixed orders draft/agent scenarios in the embedded surface remain to
be repeated. Local CA and interactive zitadeltg checks stay deferred to the agreed
handoff. Orders are still draft-only; massage is still schedule-only.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
