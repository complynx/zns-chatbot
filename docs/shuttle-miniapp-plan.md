# Embedded Mini App account bootstrap plan

The source implements proof verification, durable PostgreSQL sessions, public
HTTP endpoints and a minimal account UI. This is not yet Telegram launch acceptance.
See [the implementation contract](shuttle-miniapp.md) for configuration and gates.
Independent architecture
and Go consultations selected one bootstrap POST and an opaque application bearer.
Cookie authentication is not part of this slice: Telegram Web can embed the app
cross-site, and browser third-party-cookie behavior must not determine account access.
No IAM credential, synthetic email or administrative claims reach the browser.

## Selected protocol

The client generates a cryptographic 32-byte bootstrap nonce. A bounded same-origin
JSON `POST /app/session/bootstrap` sends the original `init_data` string and nonce.
Verify the proof before creating persistent rows or making IAM requests. The
configured bot token binds the deployment; no caller-supplied bot ID or secret is
accepted. Telegram's [Mini App validation protocol](https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app)
uses HMAC-SHA256 with `WebAppData` as the initial key and the bot token as its message,
then signs sorted decoded field/value pairs excluding `hash`. This differs from
the Login Widget protocol and from the third-party Ed25519 path. Preserve the
signed user JSON text while hashing; use only the validated user afterward.

Reject malformed query encoding, repeated query keys, ambiguous user JSON, invalid
UTF-8/control characters in keys, invalid/fractional/bot IDs, stale/future dates and
oversized input. Keep unknown signed fields in the check string. Missing `query_id`
is allowed; it depends on the launch mode. A replay digest uses the verified
canonical signed payload, so query reordering/percent-encoding differences cannot
create another proof identity.

A short transaction creates/finds the local user by bot/Telegram ID and persists
the canonical proof digest, nonce digest, random session UUID and proposed local
user UUID. Do not fabricate a Telegram update, conversation, Loom run or message to
authorize a web session. Outside the transaction, reuse `EnsureUser` with the
persisted local UUID, reconcile a lost create result, bind the identity, obtain
and verify a server-side user token. Activation then rechecks local revocation,
bot/organization/subject binding and the attempt deadline. No network call holds
a DB transaction/connection.

Derive an app-session subkey from the configured bot token with a fixed versioned
HMAC domain. Derive the opaque bearer with a different domain and fixed-width
bot ID/session UUID/decoded nonce input. Persist only its hash. A separate domain
derives a nonsecret key-generation marker; compare it against the current marker
on pending/replay/active paths. Do not rely on bearer-hash lookup alone after key
rotation. No additional signing secret or unauthenticated start-session endpoint
is required for this single-bot slice.

The same proof and nonce reconcile the same session/token and original expiry;
another nonce conflicts. Logout, failed activation and expired attempts retain a
proof tombstone for at least the proof freshness window. Active expiry is absolute;
IAM refresh never extends it. A bot-token change invalidates app sessions whose
marker differs. Reverting to the old token can make unexpired old markers match
again; this is not irreversible generation revocation. Never silently issue a new
credential for a consumed proof.

`GET /app/me` accepts only the dedicated app-session Authorization scheme and
returns a safe owner profile projection. It rechecks current local identity and
server-side IAM using the existing bounded token/cache lifecycle. A temporary IAM
outage may be hidden only by still-valid authorization; otherwise return retryable
503. `POST /app/session/logout` invalidates that session without deleting proof
consumption. These capabilities cannot authorize internal agent/admin endpoints.
Loom availability is irrelevant to web authentication.

## Browser and configuration boundary

Use one exact configured HTTPS app origin; do not infer it from untrusted Host or
forwarded headers. Require same-origin JSON POSTs and close CORS. Authorization
headers carry the app bearer; no ambient cookie authorizes a request. Keep initData,
nonce and bearer out of URLs, logs, traces, rendered text and localStorage. The
minimal client can use memory/tab-scoped sessionStorage for uncertain retries and
reloads; neither is storage for IAM tokens. Future business writes still require
their own durable operation/idempotency protocol.

The app origin is startup trust configuration in config.yaml/env. Adjustable
freshness, pending/session lifetime policy belongs in bounded DB settings and the
existing administrative update protocol. Defaults and limits are documented in the
implementation contract; event-zone deadlines do not control authentication timestamps.
Initial metadata must not overwrite an existing user's chosen profile or silently
relink a subject. Signing-key rotation and active expiry are separate from local
account revocation.

The smallest HTML surface loads Telegram's SDK, bootstraps, displays localized owner
name/session state and supports logout. No frontend framework or business forms are
required. Use no-store, no-referrer and a restrictive CSP. No public forwarding
proxy to arbitrary internal tools is introduced.

## Required acceptance

Behavioral checks cover valid/forged/wrong-bot/stale/future proofs; duplicate and
ambiguous encodings; exact concurrent retries and a different nonce; lost IAM
create/activation responses; simultaneous bot/web first login; restart; revoked
or changed binding; token-generation changes; absolute expiry/logout tombstones;
pool-size-one operation; origin/auth-scheme separation and absence of IAM secrets.
Source review loops, strict lint and bounded Linux race tests precede native QA.

Owned real Zitadel QA can use a locally signed fixture for only the authorized
current Telegram ID, without changing Loom developer stands. Clearly label that
as a synthetic signature, not Telegram-issued initData. A real Telegram launch,
iframe storage and actual initData acceptance remain separate gates after Daniel's
local CA trust step. This does not require changing production or zitadeltg first.

## Proof primitive checkpoint

Three fresh correctness/security/quality reviews found no actionable issue in the
proof/credential diff. A fixed HMAC fixture generated independently with Python
passes; behavior checks reject altered/wrong-bot/stale/future proofs, duplicate
query/JSON fields, malformed encodings and invalid IDs. Canonical reordered input
keeps the same proof digest. Credential tests cover deterministic retries,
different bot/session/nonce inputs, key rotation and strict nonce encoding. Linux
unit tests and race tests passed; final lint reported zero issues. The first unit
run failed because its expected `time.Unix` value retained a Local zone pointer;
the assertion now explicitly expects UTC. This was a test correction, not a change
to proof freshness. HTTP, stored replay/expiry/revocation and real IAM/Telegram
acceptance are not established by these primitives.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
