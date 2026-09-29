# Zitadel identity boundary

Local deployment configuration currently pins Zitadel **4.16.3**. The authorizer
configuration uses `telegram.invalid`; its implementation derives lookup email
as `tg+BOT_ID+TELEGRAM_USER_ID@email_domain`. Its external IdP subject uses the
distinct Telegram OIDC subject. Never derive that subject from a numeric bot
update sender. This was checked against the local `zitadeltg` repository and
`server_configs/configs/zitadeltg/config.yaml`; no production settings were changed.

The Go adapter implements token exchange through a confidential bot application
and authenticated introspection through a separate API application. It checks
active status, issuer, audience, application, expiry, not-before and the delegated
actor. Business services must still check their own permissions and ownership.
The bot resolves a trusted Telegram sender to a stored Zitadel subject before
exchange. Model output cannot choose the principal. No identity token enters
model input.

Use an organization-scoped impersonation service account. A test actor initially
has `ORG_END_USER_IMPERSONATOR`; users holding Zitadel administrative roles need
explicit evaluation of the separate organization admin-impersonation role.
The adapter does not request general IAM administration rights. The synthetic
email is a lookup aid, not mailbox verification or permission to merge accounts.

Sources: [token exchange](https://zitadel.com/docs/guides/integrate/token-exchange),
[introspection contract](https://zitadel.com/docs/apis/openidoauth/endpoints),
[version-pinned initialization settings](https://github.com/zitadel/zitadel/blob/v4.16.3/cmd/setup/steps.yaml).

## Test stand and current acceptance boundary

`compose.identity.yaml` starts a separate PostgreSQL and Zitadel 4.16.3 on
`127.0.0.1:8113`, with synthetic-only bootstrap settings and a separate persistent
volume. Its credentials are never production credentials. A disposable bootstrap
PAT is written under ignored `qa.local/identity`; do not use it in bot runtime.
Discovery was verified at `http://localhost:8113/.well-known/openid-configuration`.

The user explicitly approved local synthetic impersonation, applications, two users
and the scoped service account. Bootstrap has run; its persistent volume is preserved.
Credentials remain private
under ignored `qa.local/identity`; never print `state.json` or `admin.pat`.

Actual HTTP token exchange and separate API introspection passed for both synthetic
users: active status, subject, issuer, audience, client, expiry, actor, bearer type
and JWT issued type matched. Denials passed for nonexistent subject, invalid actor,
wrong audience, invalid bearer, invalid API credentials and absence of the scoped
impersonator role. The actor role was restored after the check. Sanitized evidence
and restart commands are in `qa.local/identity/acceptance.json` and `HANDOFF.md`.

The actor PAT lacks the project audience. Acceptance obtains a short-lived OAuth
actor token using the synthetic service-account client secret and explicit project
audience scope. The Go adapter now accepts `ActorClientID` and `ActorClientSecret`,
obtains that token through client credentials, shares it across concurrent exchanges,
and renews at 90% of its lifetime. Failed renewal rejects the exchange. Static
`ActorToken` remains supported for compatibility; the two modes are mutually exclusive.
The local acceptance uses client credentials, not a PAT. Verified
email alone left humans INITIAL; local bootstrap now supplies random initial
passwords. Its OIDC app pairs code response with authorization-code and exchange
grant types.

On 2026-09-26, the actual Go adapter passed exchange and authenticated introspection
for Alice and Bob against the local stand. It rejected unknown subject, invalid bearer,
invalid actor credentials, invalid API credentials, wrong audience, and wrong actor
claim. Run from `platform` with `ZITADEL_LOCAL_STATE` pointing to the private local
state file: `go test ./internal/identity -count=1`. Without that variable the live
test explicitly skips. TLS tests cover claim rejection, concurrent actor-token reuse,
renewal, failed renewal, and separate credentials. Focused tests, `go vet`, and pinned
GolangCI-Lint 2.14.0 passed. This does not establish Telegram-like end-to-end acceptance.
Runtime wiring, durable identity mapping/import, and fresh independent Code QA /
Functional Senior QA remain unaccepted. No production settings changed.

## Code review disposition

The first static review suggested removing URL escaping from HTTP Basic client
credentials. That suggestion is not applied: OAuth client authentication encodes
both identifier and secret using application/x-www-form-urlencoded before Basic
base64 encoding ([RFC 6749 section 2.3.1](https://www.rfc-editor.org/rfc/rfc6749#section-2.3.1)).
The TLS test now covers colons, spaces, plus signs, percent signs and slashes.
Separate live HTTP and Go adapter acceptance passed; independent functional acceptance remains pending.
