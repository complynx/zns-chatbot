# Production runtime contract

The Go binary now has an explicit `env: production` validation path. This is a
bounded runtime-enablement stage, not deployment authorization or full production
acceptance. Existing sandbox modes, fixtures and local Zitadel HTTP support remain
available only under their previous sandbox policy.

## Commands and identity

- `app` runs the combined application with direct OpenAI access.
- `api` runs the authenticated business API and requires direct OpenAI access for
  broadcast generation. `bot` can use that API and direct OpenAI access. Put inter-service HTTP on a private network; no new transport
  authentication or TLS termination is added by this change.
- `migrate` runs existing schema migrations using separately injected migrator
  credentials. It does not require runtime Zitadel credentials or seed fixtures.
- `health` checks local readiness without runtime credentials.
- `fake`, `fixture`, `product-fixture` and `export-fixture` are rejected.
- Standalone `model` and the `remote` model provider remain rejected in production:
  their existing HTTP boundary has no service authentication. Use `app`, or `bot`
  with direct OpenAI. `scripted`, `fixture` and `codex` providers are rejected for
  production bot/application/API execution. Fields unused by a command do not enable
  their adapters.

Business processes require `auth.mode: zitadel`, separate bot/API applications,
complete actor credentials and an HTTPS issuer. Adapter configuration receives
the runtime environment; only sandbox permits an HTTP issuer. Runtime resolves
existing trusted Telegram/owner/issuer/subject links. Opt-in trusted first-contact
provisioning creates ordinary users through a separate core-only management
credential; it never merges users by email. The authorizer link extension uses
verified OIDC subjects and requires its own integrated acceptance. See
[the identity provisioning contract](identity-provisioning.md). Keep
`auth.signing_key` for the independent delivery service
boundary; it remains at least 32 bytes.

Bot/application startup calls Telegram `getMe` before polling or maintenance and
requires a bot matching configured `auth.zitadel.bot_id`. Configuration also checks
the token's numeric prefix. This comparison is not identity provisioning and does
not prove the state of user mappings.

## Configuration and credentials

Production rejects `synthetic_only` and `parent_stdin`. The database must use an
explicit `postgres://` or `postgresql://` URL containing a username, password,
hostname and database path; URL parameters must not override credentials.
Inject runtime and migrator credentials independently. PostgreSQL transport policy
still belongs to deployment configuration; use TLS where the network requires it.

Provider/application/actor/worker credentials must be explicit, at least 16 bytes,
and free of bundled sandbox, synthetic, fixture and common placeholder markers.
These checks reject accidental test settings; they cannot prove entropy, issuance
or provider ownership. Generate independent secrets and inject them from protected
deployment configuration. Errors never include their values. No secrets are added
to an example file by this stage.

The Telegram endpoint must be explicitly set to `https://api.telegram.org`.
The client has no implicit endpoint fallback. The public Mini App URL and configured legacy browser
origins require HTTPS and a public-shaped hostname/IP; loopback/private IPs and
local/internal names are rejected. This is a syntactic check with no DNS lookup.
Configure the correct external reverse-proxy path before deployment.

## Remaining acceptance and rollback

Startup reports production policy and authentication validation failures as an
`error` object with static `code`, `field` and `reason` attributes. Field identifiers
use underscores. These diagnostics contain no configuration values or underlying
provider error. Malformed YAML, arbitrary errors and wrapped errors retain the
generic `operation failed` log value. This is a bounded diagnostic catalog, not a
promise that every configuration error has a structured explanation.

Candidate29 remains frozen and failed Functional QA for missing rejection reasons.
The diagnostic follow-on requires its own isolated verification and fresh QA; the
code change does not transfer acceptance to candidate29 or establish production
deployment readiness.

Acceptance of automatic first-contact identity provisioning and authorizer convergence, a separately reviewed Go
deployment artifact, persistence/secrets/proxy configuration and stopped-writer
data reconciliation remain required. The root publication workflow still builds
the Python image. Do not change that workflow or deploy merely because startup
validation succeeds.

Focused tests cover allowed/rejected commands, fixture/default credentials,
provider/auth/URL restrictions, HTTP issuer compatibility, and startup bot identity
verification using a local HTTP test server. They make no external provider calls.
Fresh independent Code QA and Telegram-like Functional Senior QA must assess the
complete stage against an isolated real Zitadel/Linux Docker stand; skipped checks
are not acceptance.

This stage changes no database shape or data. A code rollback restores sandbox-only
startup; do not assume restarting Python can reconcile writes made by a later Go
cutover. Production publication, migration and deployment require separate approval.

Bot and combined-app production configurations also require
`orders.active_event` (`ZNS_ORDERS__ACTIVE_EVENT`). Only sandbox mode supplies the
`sandbox-festival` default. Set the real selected event identifier explicitly;
a complete identity/provider configuration does not supply this business setting.
