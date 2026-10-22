# Synthetic Telegram proof with complete IAM login

The isolated fixture passed full OIDC authorization against real ZITADEL
v4.16.3, using the actual provider prelink hook, bot backend and PostgreSQL.
Telegram signatures/JWKS and Mini App initData were synthetic. No Telegram
authorization, Telegram message, Loom request or paid model request was made.
No production data was used.

This closes the fixture's former final-relay-stub boundary. It does not establish
the final interactive Telegram browser flow with prelink activated, production
readiness or the newly required real-name onboarding.

## Accepted paths

- Provider-first login created a canonical local/IAM identity and its explicit
  IdP link. Actual ZITADEL returned an authorization code without an external-user
  registration or manual-link form. The test exchanged it with S256 PKCE and
  confidential-client authentication, then checked the IAM ID token's RS256
  signature, issuer, audience, nonce, subject and expiry.
- Mini App bootstrap after that login resolved the same canonical account. The
  actual backend obtained and verified the user credential before returning its
  app session. This was an API check with synthetic initData, not Telegram UI.
- A second person first bootstrapped through Mini App. Subsequent provider login
  completed the same OIDC path and resolved that original account.
- A fresh sequential provider login preserved the first account and sole IdP
  link. It did not create another local or IAM user.
- After SIGTERM/restart of the backend and provider, a fresh provider login still
  completed OIDC for the same identity. This is a completed-login restart check,
  not an interrupted callback or SIGKILL recovery proof.

The accepted attempt created two users and four completed prelink receipts.
It preserved one user/receipt from the preceding failed attempt. Exact readbacks
therefore showed three local users, five completed receipts, zero pending
receipts, and four users in the separate IAM organization including its service
actor. The original failed user and acceptance artifact remained unchanged.

## Configuration failures retained

The first full-login attempt completed prelink but returned to the provider
instead of completing OIDC. The freshly created custom organization policy had
`externalLoginCheckLifetime=0s`. The correction copied the existing instance's
nonzero lifetime into that organization, preserving its other policy fields and
IdP attachment. Fresh synthetic identities then passed the paths above.

Earlier fixture setup also retained an organization-policy attachment rejected
for missing `ownerType`, and a startup URL validation failure before any tables
were created. Continuations reused the recorded IAM resources and fresh database;
they did not rerun account creation or relabel the failed attempts as passed.

Independent review loops fixed preservation of existing acceptance artifacts on
rejected replay, added the provider-first-to-Mini-App reconciliation check, and
made restart readiness tolerate transient transport failures. A fresh review of
the final acceptance and its policy continuation found no substantive findings.

## Isolation and artifacts

The fixture uses its own organization, scoped IAM actor, application database,
non-superuser PostgreSQL role, signing volume and synthetic Telegram key.
Bot/provider images are respectively
`sha256:a9c2d76e5f3a01889ef041e4bfba8131433a84241829e6cbd1656815e00869fd`
and `sha256:867e7b02b84f8f6e5e928a441557ec8070600df6d90f469a7910c1fda3df5901`.
It reuses the owned local IAM/PG/gateway; the primary application database and
Loom stand are not used. The real-Telegram baseline provider container remains
unchanged. The private prelink listener has no published host port. TLS validation
remains enabled; the root CA private key is not mounted.

Ignored controllers are under
`.cache/shuttle-work/prelink-real-iam-synthetic-20261022-v1/`.
Private receipts are under
`.cache/shuttle-live-test/zns-identity-v1/mock-prelink-v1/`, including
`acceptance-v1.json`, its preserved diagnostics, and `acceptance-v2.json`.
The directory also contains credentials and synthetic tokens; do not publish it.
Controllers refuse replay. Reconcile recorded effects after interruption instead
of rerunning prepare/start/exercise against an existing receipt.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
