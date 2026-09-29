# Optional Zitadel runtime authentication

The default empty or `sandbox` authentication mode preserves the local synthetic
signer. Set `auth.mode: zitadel` explicitly to enable the real identity adapter.
`env: production` requires the real adapter and the restrictions in the
[production runtime contract](production-runtime-contract.md). This bounded
runtime path is built but not independently accepted for deployment. Existing
sandbox behavior remains available with `env: sandbox`.

Configure these fields under `auth.zitadel` (or the matching
`ZNS_AUTH__ZITADEL__FIELD` environment variable):

- `issuer`: exact issuer origin, no trailing slash.
- `audience`: project audience expected by introspection.
- `bot_id`: quoted positive numeric Telegram bot ID used by the existing authorizer.
- `bot_client_id`, `bot_client_secret`: separate confidential robot application.
- `api_client_id`, `api_client_secret`: API introspection application.
- `actor_id`, `actor_client_id`, `actor_client_secret`: scoped impersonation actor.

Both split processes and the combined application currently require the complete
adapter configuration. Keep `auth.signing_key` for the independent notification
delivery service boundary. App and actor secrets use the redacting configuration
type and are registered with the runtime logger. Tokens never enter model input.

Until automatic first-contact provisioning is implemented and accepted, provision
`core.users`, `core.zitadel_identities` and `core.telegram_identities` through the
operator importer before enabling this mode. `identity.Links.Bind`
refuses mismatched or reassigned bindings. Runtime never creates accounts, derives
an OIDC subject from a Telegram number, or merges users using email claims. The
configured bot ID is an operator-provided namespace; confirm it belongs to the
Telegram polling token. The local synthetic transport can use its own test token.

Polling updates must pass the existing private-chat sender checks. Mini App
requests must pass signed Telegram initData verification. Both then resolve the
trusted Telegram sender to a stored owner and Zitadel subject. Each business API
request exchanges the subject again; requests cannot substitute another owner.
The API introspects the bearer, resolves its linked owner and applies existing
database permissions for manual and agent operations. Revoked or missing mappings
fail closed. A mapping database outage leaves the durable update pending for retry.

Scheduled notification recipients originate in the authenticated service queue or
durable database. Before reading user preferences or rendering refreshed cards,
their Telegram ID must resolve to the same stored recipient owner. Delivery queue
operations retain their separate service token. A synthetic user bearer cannot
authenticate a business request while Zitadel mode is enabled.

Periodic workflow, pass, order, profile, knowledge, media and massage card refreshes
also resolve each durable owner/chat pair before user API reads. Each card uses a
separate context; a missing or revoked link leaves that card pending while other
users continue refreshing. Restoring the link permits the next cycle to retry.

Massage reminders persist recipient-attempt ordering across polling cycles and
restarts. A recipient with an unavailable identity remains pending without
continually taking the earliest recipient slot. Order notifications choose the
earliest available attempt before its ID, so expired retries do not starve
newly available deliveries. These mechanisms preserve pending work; they do not
treat identity failures as successful delivery.

Focused tests cover principal substitution, absent/partial configuration, lookup
outages, exchanges per request, real PostgreSQL links, existing owner permissions,
Mini App ownership, mapping revocation and separate notification credentials.
Provider stubs test runtime wiring; adapter TLS/live tests cover OAuth and claims.
These checks do not replace independent Telegram-like functional acceptance.
