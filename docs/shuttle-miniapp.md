# Embedded account session

The embedded Mini App provides account bootstrap, profile and logout plus domain forms through owner-scoped APIs. IAM calls stay server-side. Saving or reading a form does not start an unsolicited Loom run. See [registration details](shuttle-miniapp-registration-details.md), [order selection](shuttle-orders-selection.md) and [account status](shuttle-account-status.md).

## Enablement

The origin contract is configured through `mini_app_origin` in the
startup YAML, or `SHUTTLE_MINI_APP_ORIGIN`, to one exact HTTPS origin such as
`https://app.example.invalid`. An empty value disables all `/app/` routes.
Use lowercase hostnames, omit the default HTTPS port, and include no trailing
slash, path, query or credentials. Noncanonical hostname case and explicit `:443`
are rejected at startup. Proxy this origin to the public HTTP listener; internal
agent endpoints must stay on the separate private listener. Do not derive the
trusted origin from request headers.

The configured Telegram token must belong to the configured bot. Serve the page
over trusted HTTPS and configure its launch URL in Telegram separately. Enabling
the routes does not configure BotFather or prove a Telegram WebView launch.

## Local Telegram launch redirect

Environment overrides are `SHUTTLE_MINI_APP_ORIGIN`,
`SHUTTLE_MINI_APP_LAUNCH_URL` and `SHUTTLE_MINI_APP_ALLOW_LOCAL_HTTP`.
An explicit false environment value overrides a true YAML flag. These are startup
parameters; changing them requires an application restart.

`mini_app_origin` is the exact origin checked for session and form requests.
The local HTTP exception requires the explicit flag and the exact lowercase
hostname `localhost`. IP literals and localhost subdomains are rejected. Ports
must be canonical and valid; explicit default ports, paths, credentials, queries
and fragments are rejected. Production HTTPS validation remains unchanged.

`mini_app_launch_url` is the HTTPS address used in native Telegram launch buttons.
It defaults to the configured HTTPS origin plus `/app/`. An override requires an
enabled Mini App origin and a canonical HTTPS URL with a clean path ending in
`/app/`; path prefixes are supported. Queries, fragments and credentials are not
allowed. A local HTTP origin requires an explicit HTTPS launch URL. The launch
host does not become a trusted request origin.

Runtime settings use the existing administrative settings update protocol:

| Field | Default seconds | Allowed seconds |
| --- | ---: | ---: |
| `mini_app_max_age_seconds` | 300 | 30–900 |
| `mini_app_pending_seconds` | 120 | 30–300 |
| `mini_app_session_seconds` | 900 | 60–3600 |

Zero or absent fields use defaults, so old settings remain readable. Future clock
skew is a fixed 30 seconds. Session expiry is absolute, independent of IAM refresh.

## Public protocol

- `GET /app/` serves the embedded page and its shipped, explicitly allowlisted assets. The allowlist is defined by the current application routes.
- `POST /app/session/bootstrap` accepts JSON `init_data` and `bootstrap_nonce`.
  The nonce is 32 random bytes encoded as canonical unpadded base64url. The proof
  is validated before DB access or IAM calls. The response contains the local
  user UUID, chosen full name, bot language, absolute expiry and an opaque
  `session_token`. This is an application capability, never an IAM token.
- `GET /app/me` uses `Authorization: ZinusyaSession <session_token>` and returns
  the same safe profile projection without a token field. Current local binding,
  revocation and server-side IAM authorization are checked.
- `GET /app/account` adds bounded owner registration/pass/payment-status reads;
  [the separate contract](shuttle-account-status.md) specifies its limits.
- `POST /app/session/logout` uses the same scheme and JSON `{}`. A 204 consumes
  the session. It does not require IAM availability.

The [profile contract](shuttle-profile-expectations.md) adds owner-only
`GET /app/profile` and `POST /app/profile/name` / `language`. Name writes use
`operation_id`, `expected_version`, `first_name` and `last_name`; the form has
separate first/last inputs. Components may contain spaces. Legacy full-name
strings are displayed without guessing their components. A committed change
queues a name-only IAM update; the form confirms the local save, not remote
IAM completion. The same operation and both components are retained for an
uncertain retry after reload. These explicit form actions do not start a
proactive agent run.

POST requests require the exact configured Origin and JSON content type. GET
requests with an Origin must also match. There is no CORS or cookie authentication.
Responses are no-store. Bootstrap bodies are bounded at 64 KiB, raw proofs at
16 KiB; authentication work has a five-second context timeout. Errors contain no
credentials. Invalid proof/session returns 401, conflicting proof reuse 409, and
temporary backend failure 503 with a retry hint.

The client saves its nonce before the request. Retrying the same canonical proof
and nonce recovers the same session, bearer and original expiry. Another nonce
cannot reopen a consumed proof. A short transaction persists the pending claim
before IAM work, so a lost IAM create response can be reconciled after restart.
No DB connection is held during network calls. Existing bot profile metadata is
preserved; web login creates no Telegram update, conversation, task or Loom run.

Logout and expiry retain proof tombstones. Bounded lazy cleanup waits at least
24 hours after proof authentication and also requires pending/session expiry;
raising the runtime freshness policy cannot resurrect a consumed proof. Migration
24 refuses rollback while session rows remain; export/retention must be deliberate.
Bot-token changes invalidate sessions through a stored key-generation marker.
Restoring the old bot token can restore a matching unexpired marker; this is not
an irreversible key revocation mechanism.

## Browser lifecycle

For session authentication, the client keeps its bearer, nonce and a SHA-256 launch fingerprint in
memory/tab-scoped sessionStorage. Raw initData and IAM credentials are not stored
there. A changed launch discards the previous local capability and bootstraps the
new signed proof, which prevents account-switch reuse. A same-launch reload reads
the existing session. A rejected stored capability gets at most one bootstrap
attempt; a consumed proof requires reopening from Telegram. Transient failures
preserve the same attempt. Outside Telegram, the page does not resume a saved
capability. Names are rendered as text. English and Russian are supported, with
Belarusian/Ukrainian interface fallback to Russian.

These are browser lifecycle safeguards, not identity validation: only the server
trusts signed Telegram identity. Session authentication can retry in memory when
storage is unavailable during that page lifetime. The [orders draft form](shuttle-orders-selection.md#form-behavior)
has a separate contract: it persists owner/event IDs, operation UUID, versions and
exact quantities before dispatch, and blocks new saves if that storage fails. CSP permits only the app assets and official
Telegram SDK, with Telegram Web as the allowed iframe ancestor. Actual Telegram
iframe/storage behavior remains a separate acceptance gate.

## Styling rules

Design for a vertical Telegram WebView first. Check a narrow portrait viewport before desktop layouts. Use Telegram CSS variables for application colors: `--tg-theme-bg-color`, `--tg-theme-text-color`, `--tg-theme-hint-color`, `--tg-theme-link-color`, `--tg-theme-button-color` and `--tg-theme-button-text-color`. Keep product colors for artwork such as avatar frames. Supply neutral fallbacks in `var(...)`; do not redefine Telegram theme variables with fixed application colors.

Respect `--tg-safe-area-inset-*` and `--tg-content-safe-area-inset-*` so controls stay clear of device and Telegram chrome. Size fixed bottom controls using `--tg-viewport-stable-height` rather than the changing viewport height. Keep controls usable when the keyboard, theme or WebView size changes. Use native Telegram MainButton and BackButton where the flow supports them, with accessible browser fallbacks.

Theme, viewport and safe-area values come from the official Telegram SDK. They are presentation data, never authentication or permission evidence. See the [Telegram Mini Apps reference](https://core.telegram.org/bots/webapps).
