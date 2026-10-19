# Embedded account session

The initial session slice provides account login and logout for a Telegram Mini
App. Later slices add [protected registration fields](shuttle-miniapp-registration-details.md)
and an [orders draft form](shuttle-orders-selection.md), each with its own rollout
and acceptance boundary. The orders draft form and native /orders entry are installed on the owned
runtime at schema38, subsequently upgraded to schema39. [Four-path acceptance](shuttle-orders-selection-qa-2026-10-16.md)
uses actual Telegram, real IAM/Loom and direct form APIs with locally signed proofs
and a manual provider; trusted WebView and unattended model checks remain open. It does not
submit orders or payments. A subsequent read-only
[owner registration/status view](shuttle-account-status.md) is implemented with
its own acceptance boundary. IAM calls remain server-side; Loom is
not involved. Architecture decisions are in [the plan](shuttle-miniapp-plan.md).

## Enablement

Apply migration 24 before starting this candidate. For production, set `mini_app_origin` in the
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

The owned desktop test stand can use the existing server redirect:

```yaml
mini_app_origin: "http://localhost:18382"
mini_app_launch_url: "https://complynx.net/testbot/18382/app/"
mini_app_allow_local_http: true
```

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

Python `full_link` in `zns-chatbot/config.py` uses this same redirect mechanism.
A read-only live probe on 2026-10-16 confirmed that
`https://complynx.net/testbot/18382/app/` returns 302 to
`http://localhost:18382/app/`, preserving query parameters. The nginx configuration
in `server_configs/configs/complynx/nginx/complynx.conf` also supports alphabetic
localhost subdomains, but the Go HTTP exception intentionally accepts only
`localhost`. No server configuration was changed.

The source implementation passed independent reviews, a broad Linux race gate
of 118 tests (205 pass events), and a final focused gate of 18 tests (72 pass
events) after the configuration helper was changed to a free function. Strict
lint passed. Both gates preserved their frozen source inputs and the ten main
stand services; disposable containers and volumes were removed. The first broad
attempt had an incorrect subprocess-helper inventory and later lint issues;
those results remain separate from the passing gates.

The owned runtime now includes this configuration at schema39. Actual Telegram
initData authentication, a form save, reload recovery and logout passed in a
separate top-level browser tab opened with the exact Telegram-generated launch
URL. After Daniel approved the browser permission, the same Telegram Web iframe
passed opening, authenticated reads, draft writes, logout and re-entry. Mixed
agent/form workflows and payment-review writes there remain open. See the [local launch QA record](shuttle-miniapp-local-launch-qa-2026-10-16.md). Loopback means the machine running Telegram;
this does not expose the developer's server to a remote phone. Zitadeltg CA/provider
interactive acceptance remains a separate handoff.

Runtime settings use the existing administrative settings update protocol:

| Field | Default seconds | Allowed seconds |
| --- | ---: | ---: |
| `mini_app_max_age_seconds` | 300 | 30–900 |
| `mini_app_pending_seconds` | 120 | 30–300 |
| `mini_app_session_seconds` | 900 | 60–3600 |

Zero or absent fields use defaults, so old settings remain readable. Future clock
skew is a fixed 30 seconds. Session expiry is absolute, independent of IAM refresh.

## Public protocol

- `GET /app/` serves the page. Source exposes only app.js, orders.js,
  payment-reviews.js and app.css. All four are included in the current owned
  schema39 candidate.
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

## Verification boundary

Independent proof reviews and DB correctness/security/quality reviews found no
actionable defects. HTTP/UI review found stale-capability handling, launch/account
isolation and noncanonical Origin configuration defects; these were fixed. A fresh
lifecycle review found no actionable issue. The post-correction full Linux race
gate passed: all 164 enumerated integration tests in 11 fresh bounded groups,
plus the explicit cmd/internal/migrations packages. Strict lint reported zero
issues. Seven dependency-free Node
behavior tests now pass, including account switching, same-attempt lost replies,
transient IAM failure, rejected sessions and logout.

Owned native real-IAM fixture acceptance passed; see [the native QA record](shuttle-miniapp-qa-2026-10-15.md).
The later [local launch check](shuttle-miniapp-local-launch-qa-2026-10-16.md) proves
Telegram-issued initData in both a top-level tab and the Telegram Web iframe after
browser permission approval. Embedded draft writes and logout/re-entry also pass.
It does not prove local trusted HTTPS or production readiness. Daniel's
local CA installation and later interactive zitadeltg checks remain deferred.

Earlier test failures are preserved in local evidence: an ambiguous SQL column was
qualified; a test compared time-zone representations instead of instants; an
internal-auth test supplied no body and reached JSON validation before auth; lint
found fatal test assertions in a worker goroutine, which were moved to the test
goroutine. These are separate from later passing results.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
