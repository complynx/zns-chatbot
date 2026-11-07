# Trusted Telegram provider prelink

## Outcome and current boundary

Bot, Mini App and zitadeltg must resolve one local user and one active IAM account
for `(configured bot ID, verified numeric Telegram ID)`.
Telegram OIDC's opaque
subject is a separate value; never reconstruct it from the numeric ID.
The explicit IAM link adapter is implemented in `internal/identity/links.go`.
The [receipt migration and local transactions](shuttle-telegram-prelink-storage.md)
are also implemented and tested on isolated PostgreSQL.
The [provider assertion verifier](shuttle-telegram-prelink-verifier.md) is also
implemented and independently reviewed.
The [private endpoint and startup wiring](shuttle-telegram-prelink-api.md) now
pass scoped real-PostgreSQL integration checks.
The [isolated provider hook](shuttle-zitadeltg-prelink-provider.md) has reviewed
source and Linux/race component checks.
Main provider/backend activation and real interactive login remain open.
Real browser/Telegram OIDC acceptance remains separate.

This design follows read-only architect/security and Go senior consultations.
Keep one synchronous retryable prelink operation. It needs no new agent loop,
queue, distributed lock service or worker. Zinusya owns identity/business state;
Loom is not involved in logging in or linking accounts.

## Trust contract

A dedicated backend-only `POST /internal/identity/telegram-prelink` accepts one
compact signed assertion.
Authenticate the caller first with a dedicated random
32-byte service bearer, compared in constant time.
Use server-authenticated TLS,
no redirect following and a bounded request/response size.
Do not reuse Bot API,
IAM actor, browser, Mini App or human administrator credentials.
Do not expose a
generic browser/agent proxy to this operation.

The assertion uses the provider's existing RSA signer, but a separate configured
prelink audience and explicit `urn:zinusya:purpose` claim equal to `telegram-prelink`. The
browser relay JWT targets IAM and is not accepted as prelink proof. Require RS256,
one allowed key ID from configured provider JWKS, exact issuer, exact singleton
audience, `jti`, `iat`, `nbf`, `exp`, lifetime at most60s and explicit5s clock skew.
Ignore caller-supplied key URLs; key refresh can only use the configured bounded
JWKS endpoint. Startup pins bot, organization and IAM IdP; the request cannot
choose a target IAM user, organization or IdP.

Require canonical numeric bot/user claims and the actual opaque Telegram subject
from verified provider claims. Check composed assertion subject against
`telegram:BOT_ID:OPAQUE_SUBJECT`, and recompute the exact reserved address
`tg+BOT_ID+NUMERIC_USER_ID@telegram.invalid`. Signed names are initial profile
metadata, not permission grants. The provider may issue this proof only after
validating Telegram signature, issuer, audience, time and login nonce.

## Durable operation

1. After proof verification, upsert the existing canonical local user without
   creating a conversation/task or replacing established profile metadata.
   Persist its proposed UUID before any IAM call. Reject local revocation and
   conflicting organization/subject. The existing bot/Mini App pattern already
   supplies the durable UUID without holding a DB connection over HTTP.
2. In the same short transaction reserve a receipt keyed by `(provider issuer,
   assertion jti)`. Store the compact assertion SHA-256, local user, configured
   IdP, exact external subject, expiry and pending/completed state. Reused JTI
   with different bytes or intended scope is a conflict. A pending receipt proves
   intent only. Do not store raw assertions or service bearers in receipts/logs.
3. Release DB locks/connections. If the local user is already bound, verify that
   exact IAM account rather than selecting another account by email. Otherwise
   reuse `EnsureUser` with the persisted UUID. Add the explicit IdP link and read
   back the exact tuple on that account. Never move, unlink or merge a foreign
   identity. Malformed success replies and transport failures are uncertain;
   a successful status or409 alone is not acceptance.
4. Lock USER then RECEIPT, recheck current revocation, proof freshness, immutable
   intent and unchanged binding; conditionally bind the subject/organization and
   complete the receipt atomically. On lost local completion reply, repeat the
   same proof while fresh and reconcile both local and IAM state. A completed
   receipt still requires current authorization/link checks before success.

Migration33 adds a partial unique index on local
`(bot_id, zitadel_org, zitadel_subject)` where subject is nonempty.
Existing
duplicate bindings stop migration for explicit review, never automatic merging.
Concurrency can issue the same immutable IAM operation
more than once; remote uniqueness plus exact read-back reconciles it.
There is no
DB lock across network calls.
Expired proofs cannot initiate or resume mutation;
a fresh assertion may reconcile the same canonical identity and existing link.

## Provider hook and limits

In an owned source snapshot, call prelink after Telegram verification and before
issuing/relaying the IAM JWT. Retain identical compact proof/JTI for transport
retries. No successful matching receipt means no relay. Keep the sibling checkout
unchanged. A fresh browser attempt may mint a new JTI because account/link scope
remains deterministic. No global email auto-linking, automatic external account
creation or automatic profile updates are required.

Local revoke currently does not remove IAM roles. A remote link can be written
while local revocation races the final transaction; final prelink must fail and
withhold relay. Already-issued IAM relay assertions have a bounded independent
window. This is not atomic IAM-wide revocation; stronger revocation requires an
explicit IAM-side policy and separate acceptance.

## Acceptance

Use synthetic provider signatures and real owned IAM for backend acceptance:
bot-first/provider-first/Mini-App-first and concurrent variants converge on one
UUID/account/link; deliberately unrelated opaque subject and numeric ID remain
separate. Exercise lost create/link/completion replies, repeat proof/digest
conflicts, revocation between reserve/complete, wrong caller/key/issuer/audience/
bot/time, foreign binding and incomplete IAM link listing. Confirm rejection
preserves both accounts' link ownership. Block the IAM response while another
DB operation obtains a connection. Test provider failure withholding relay.

Real Telegram Web Login credentials, browser CA trust, actual signed Telegram
proof, same-origin cookie relay and interactive navigation are additional gates;
synthetic backend proof cannot close them. Orders/massage HTML workflow work
continues after the shared trusted identity/session foundation.

## API references

Wire paths follow the official [AddIDPLink](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.AddIDPLink)
and [ListIDPLinks](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.ListIDPLinks)
contracts.
