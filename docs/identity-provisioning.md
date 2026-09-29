# Retained identity provisioning

`platform/identityprovision` is the shared runtime and migration boundary. It
accepts numeric Telegram identities only from authenticated transport adapters or
reviewed migration input. It must not be exposed as a model tool or an anonymous
HTTP endpoint.

`Service.PrepareTelegram` commits an opaque local owner, reserved provider ID and
operation identifier before it calls Zitadel. It creates and verifies the provider
human only. It does not insert application users, profiles or links. The removable
users importer can therefore retain its existing atomic user/profile/link/receipt
transaction and independently chosen `can_book` policy.

`Service.EnsureTelegram` uses the same reservation, then commits the new local
user, profile, both identity links and ready marker together. Concurrent deliveries
serialize on the durable reservation row. The row is committed before the provider
request, so rollback or process loss cannot discard the reserved ID. A provider
success followed by an error is resolved by reading this exact ID and checking its
organization, human/active state and operation against the local journal. There is
no email search or metadata-only adoption. Existing durable bindings are read
without overwriting user fields or eligibility; a conflicting journal is rejected.

New trusted users get `can_book=true`. This preserves Python's ordinary unbanned
first-contact behavior (`zns-chatbot/telegram.py`, `TGUpdate.get_state` and
`parse_message_task`). Replays never reset an imported or revoked policy. Existing
unlinked local accounts require explicit reconciliation, rather than automatic
creation of another provider account.

The adapter uses official generated `zitadel-go/v3` v3.29.2 UserService APIs, whose
release targets Zitadel4.16.0. It uses TLS and a reused OAuth token source. Local
HTTP requires explicit opt-in and a loopback hostname/address. API calls have
bounded deadlines. Configure a separate organization-scoped provisioner token
source; do not reuse the impersonator, model or API-introspection credentials.
`ORG_USER_MANAGER` includes permissions beyond creation. A disposable principal
with only that organization role passed local Zitadel4.16.3 creation, metadata
readback and external-link operations. This proves sufficiency for those tested
operations, not theoretical role minimality. No IAM administrator role is
required by this implementation.

Creation sends a random initial password, an unverified synthetic `.invalid`
email with return-code delivery suppression, and no external IdP links. Neither
password nor returned email code is logged or persisted. Local Zitadel4.16.3
proof established that this creates an ACTIVE human usable by the existing scoped
impersonator without claiming mailbox ownership. Repeated creation with the same
ID returned HTTP400/gRPC FailedPrecondition; recovery must use exact readback,
not merely recognize AlreadyExists. Disabled users are denied, never reactivated.

This core does not invent Telegram OIDC subjects. Authorizer integration must
validate Telegram OIDC first, call a narrow trusted ensure/link operation using
the same numeric identity, then add the exact external subject before relaying
Zitadel's callback. The authorizer handshake and external-link adapter described
below require their own QA gates. The removable operator CLI is a separate slice.
Until these gates pass, same-person browser/bot onboarding is not accepted.

Runtime onboarding is opt-in through `auth.zitadel.provisioning.enabled`. The
combined app and core API also require `organization_id`, `email_domain`,
`client_id`, and `client_secret`. These are separate management credentials;
the split bot configuration needs only the enabled flag and its existing bot ID.
The OAuth client uses the reserved Zitadel API audience, bounded requests, and
does not follow redirects.

Accepted private Telegram messages and callbacks, and verified MiniApp initData,
can request onboarding after an unknown identity lookup. The core endpoint
`POST /internal/identity/telegram` requires the existing service signer with a
separate audience, a one-minute lifetime, and an exact body digest bound to the
configured bot ID. Ordinary bearer tokens cannot invoke it. Recipient lookup,
notifications and MiniApp cookies do not create identities. Successful creation
continues the same incoming request. Provider errors remain sanitized and do not
become user tokens or model input.

Validation includes generated SDK request tests and disposable PostgreSQL tests
for concurrency, lost responses, provider success before local failure, resumable
reservations, immutable conflicting bindings, disabled users and preserved
eligibility. No production configuration changes are part of this package.

Sources: [SDK release](https://github.com/zitadel/zitadel-go/releases/tag/v3.29.2),
[pinned UserService contract](https://github.com/zitadel/zitadel/blob/v4.16.3/proto/zitadel/user/v2/user_service.proto),
[pinned role defaults](https://github.com/zitadel/zitadel/blob/v4.16.3/cmd/defaults.yaml).
The [dependency assessment](identity-dependencies.md) records pinned versions,
licenses, upstream maintenance, downstream use and remaining scan requirements.

## Authorizer convergence contract

The core API can enable `auth.zitadel.provisioning.authorizer` with explicit
`issuer`, `jwks_url`, and Zitadel JWT `idp_id`. All three fields are required
together. Production trust URLs require public HTTPS; sandbox loopback HTTP
requires explicit opt-in. The split bot does not need these fields.

Configure the corresponding authorizer bot's complete `identity_backend_url` as
`https://<core-origin>/internal/identity/authorizer`. The authorizer calls this
endpoint after it validates Telegram OIDC and before it relays its ordinary
Zitadel JWT. It sends `{"assertion":"<RS256 JWT>"}` and requires
`{"ready":true}`. The backend never accepts actor fields outside the assertion.
The authorizer requires `synthetic_email_verified: false` for backend-linked
bots and rejects a configuration that would claim synthetic mailbox verification.

Required claims are relay `iss`, the single audience `zns-identity-provisioner`,
`purpose=telegram_identity_link`, `iat`, `nbf`, `exp`, nonempty `jti`, and a lifetime
no longer than 60 seconds. The namespaced string claims are
`urn:zitadeltg:telegram:bot_id`, `urn:zitadeltg:telegram:subject` (actual OIDC sub),
and `urn:zitadeltg:telegram:user_id` (canonical positive numeric Telegram ID).
JWT `sub` must equal `telegram:<bot_id>:<actual OIDC sub>`. Optional profile claims
are `given_name`, `family_name`, and `language_code`. Numeric Telegram IDs are
never substituted for missing OIDC subjects.

Verification uses pinned `golang-jwt/jwt/v5` v5.3.1 and `keyfunc/v3` v3.8.2.
Only the configured JWKS location is trusted. Fetches have bounded response
size/time, no redirects, and a cancellable refresh lifecycle. JWT header URLs
cannot choose a key source. Errors do not expose assertions or provider bodies.

`ExternalLinker.EnsureExternal` first resolves the same durable numeric identity,
then reserves its exact provider/owner/IdP/external-subject tuple in migration062.
It commits that reservation before calling the official SDK `AddIDPLink` method.
Retries inspect the exact provider user's links even after ambiguous errors.
Conflicting subjects for the same IdP, a subject reserved by another person, and
removed previously-ready links fail closed. The operation neither replaces links
nor merges accounts by email. Browser-first and bot-first calls therefore use the
same core provisioning reservation. Ready is returned only after exact external
link readback and the local ready commit.

Permanent bot onboarding `409 identity_conflict` responses are terminal for that
incoming update. The bot gives a localized, best-effort account-unavailable
response and continues the inbox without granting access. Transport/503 failures
remain pending for retry. Other protocol failures are not treated as user denial.
Database INSERT failures also remain retryable; only a verified immutable
binding mismatch becomes an identity conflict.

The authorizer extension and the repaired ingress require fresh integrated gates,
paired authorizer tests, and independent Code/Functional QA. Candidates33 and35
remain frozen with their rejection evidence; the successor has not passed
acceptance.

Browser deployment: [Telegram identity with Login V2](identity-login-v2.md) describes the supported v4.16.3 composition that preserves unverified synthetic email. Independent Functional QA of that composition remains required.
