# Browser authentication

The Go meal editor (`/miniapp/?order_id=...`) and massage timetable
(`/massage_timetable` or `/miniapp/massage`) accept either signed Telegram
Mini App initData or a browser session approved in the user's private bot chat.
Enter the Telegram username, compare the displayed request code, and approve or
decline the matching Telegram message. Both languages are available in the page.

The username is only a delivery address, resolved by a narrow Core service route.
The bot role does not read Core SQL. Every callback binds the sender to one random
request, and every session access resolves the current Telegram identity again.
Domain APIs still authorize current event, owner and administrator permissions.

Requests expire after five minutes. A new request from the same browser cancels
its previous pending request. Cancel and decline are terminal. Three requests per
recipient and 1,000 total requests are allowed per five minutes. Sessions have a
fixed 24-hour expiry; polling a completed request never extends that expiry.
PostgreSQL retains requests and session revocation across process restarts.
The browser retains the request ID in session storage and its independent secret
in an HttpOnly cookie. Responses are not cached. Sign out revokes the session.

First-party cookies use Path=/miniapp, HttpOnly, SameSite=Strict and Secure on
HTTPS. Plain HTTP is restricted to loopback for local stands. Cookie writes
require the configured Web App origin. Authentication endpoints also require the
browser's custom header; no reflected or wildcard CORS is enabled.

## First-party HTTP contract

All `/miniapp/auth/` requests require `X-Browser-Auth: 1`. Send `Origin` equal
to the configured Web App origin. A browser GET without `Origin` may instead
carry `Sec-Fetch-Site: same-origin`. Keep browser cookies on all requests.
Responses are JSON with `Cache-Control: no-store`; errors use `{ "code": ... }`.

| Method and path | Input | Successful response |
| --- | --- | --- |
| `POST /miniapp/auth/start` | JSON `{ "username": "alice" }`, maximum 1 KiB | `{ "request": "opaque-id", "result": "pending" }` plus HttpOnly request cookie |
| `GET /miniapp/auth/status?request=opaque-id` | Request ID and its browser cookie | `{ "result": "pending\|approved\|declined\|cancelled\|expired" }`; approval sets the session cookie |
| `POST /miniapp/auth/cancel?request=opaque-id` | Request ID and its browser cookie | `{ "result": "cancelled" }` |
| `GET /miniapp/auth/check` | Session cookie | `{ "result": "authorized" }` |
| `POST /miniapp/auth/logout` | Session cookie, if present | `{ "result": "cancelled" }`; session cookie expires |

The status values in the table are alternatives, not a literal combined string.
Missing/foreign browser binding returns 401. Invalid username/input or unavailable
recipient returns 400; rejected origin/header returns 403; rate limit returns
429; temporary start/cancel/logout failure returns 503. Start accepts an optional
leading `@` and trims surrounding whitespace. The request ID alone never grants
access. Session checks return 401 for missing, expired or revoked authorization.

## Legacy client adapter

`GET /auth?check=1` returns `{ "result": "authorized" }` (200) or
`{ "result": "unauthorized" }` (401). `GET /auth?username=...` waits for the
same Telegram approval and returns authorized only after consent. Decline,
expiry and cancellation return 401. Unknown names return 404; rate limits 429.
The adapter does not flush HTTP headers before the decision: the historical
Mealty userscript waits for the fetch response headers, not its JSON body.
Disconnect cancels pending legacy consent. Restart interrupts that HTTP request;
the old client must retry, and an old callback cannot approve the new request.

Third-party access is disabled unless `auth.legacy_browser_origins` (environment
`ZNS_AUTH__LEGACY_BROWSER_ORIGINS`) contains space-separated, unique, exact HTTPS
origins, for example `https://www.mealty.example`. Paths, wildcards, userinfo and
duplicates are rejected. The configured Web App URL must also use HTTPS.
Only those origins receive credentialed CORS. Origin validation protects the old
GET client, which has no custom CSRF header. Telegram shows the actual requesting
origin. Separate legacy cookies use HttpOnly, Secure and SameSite=None only for
the allowed cross-site flow; their consent is bound to that origin. Legacy
session cookies use Path=/ for the historical sibling browser routes.

Browsers that block third-party cookies can prevent that historical userscript
from retaining a session. The first-party editor/timetable remain available.
This authentication stage does not itself implement the legacy food API.

## Synthetic acceptance configuration

Use a separate disposable database and the public topology in
[product-sandbox.md](product-sandbox.md). Run `zns migrate`, then
`zns product-fixture`, with `ZNS_ENV=sandbox`; there is no `seed` subcommand.
The fixtures provide Alice/101, Bob/202 and Visitor/303. They do not reset
permissions on repeated invocation.

Set `telegram.web_app_url` (`ZNS_TELEGRAM__WEB_APP_URL`) to the externally
reachable `/miniapp/` URL. Its origin is the first-party consent origin. The
Fake Telegram proxy topology also needs `sandbox.mini_app_url` to point at the
internal app URL. Use the optional username field in the Fake Telegram input
form to supply the sender metadata that real Telegram provides. Omitted or empty
username retains its normal meaning; the stand does not invent one.

The active order event is `orders.active_event`
(`ZNS_ORDERS__ACTIVE_EVENT`). Changing it requires restarting the app or bot
with the same database. It must not redirect a previously bound callback to
another event. Keep the tested runtime image fixed when changing synthetic
configuration or permissions.

## Migration and rollback

Migration 054 adds only `bot.browser_auth` and its indexes. Deploy after normal
schema migration with the existing bot-schema grants. No legacy data is changed.
The signing key must remain stable over restarts. Old and new application versions
can read their own authentication paths independently; the deployment still uses
the required stop-old/start-new single-process sequence. Roll back the application
without dropping the new table; old versions ignore it. Do not remove pending or
session records during rollback. Expired records are reclaimed on the next start
request. No production rollout is authorized by these local checks.

Two local G124 suppressions cover the explicit loopback HTTP cookie exception;
the legacy cookie constructor additionally supports the opt-in cross-site flow.
Independent Code QA must review these policy boundaries. Tests cover the exact
allowlist, cookie attributes, callback binding, cancellation, expiry, session
replay/restart, bot/Core SQL separation, and real PostgreSQL state transitions.
