# Private Telegram prelink API

## Implemented boundary

It is registered only on the internal
handler when startup configuration is complete.
It is absent from the public
handler and agent tool manifest.
This is service authentication infrastructure,
not an operation a user or agent can invoke using an IAM/app bearer.

The [isolated zitadeltg hook](shuttle-zitadeltg-prelink-provider.md) has reviewed
source and Linux/race component checks.
Main provider/backend
activation and real interactive login remain open.
The original
checks below use real PostgreSQL and HTTP IAM/Loom peers; the separate native
fixture linked above adds real IAM for the stated endpoint/provider paths, not
real Telegram or final IAM login-session acceptance.

## Startup and caller contract

`telegram_prelink` in config.yaml has five fields: `issuer`, `jwks_url`,
`audience`, `provider_id`, `service_bearer`. All empty disables the endpoint;
partial configuration fails startup. Env overrides use
`SHUTTLE_TELEGRAM_PRELINK_ISSUER`, `_JWKS_URL`, `_AUDIENCE`, `_PROVIDER_ID`,
`_SERVICE_BEARER`. Events and business settings still belong in DB.

Issuer/JWKS must use HTTPS without credentials, query or fragment. Audience must
be dedicated, different from the IAM API and Loom audiences. The service key is
an unpadded canonical base64url encoding of 32 random bytes; it must not reuse a
Telegram, webhook, actor, exchange or introspection secret. The startup config
validates credential separation. API construction also checks that its bot/org/
IAM issuer match the actual IAM client's deployment scope. No startup network
request is required to validate these options.

Authenticate with one `Authorization: Bearer SERVICE_KEY` header. The endpoint
compares fixed-size SHA-256 digests in constant time before reading body, fetching
keys or accessing DB. It rejects browser Origin/Fetch-Site requests, emits no CORS
permission and uses `Cache-Control: no-store`. No service secret or raw assertion
is returned or logged by the handler.

The provider caller must use server-authenticated TLS and refuse redirects.
The internal handler permits TLS termination at a reviewed private gateway; it
does not infer TLS trust from forwarded headers.
Configuring HTTPS proof/JWKS URLs
does not secure the caller-to-bot connection.
Do not enable this endpoint through
the public IAM/app entrypoints or a public plain-HTTP port.
The reviewed gateway
now has a separate unpublished identity listener.

## Request and response

Use `Content-Type: application/json` with exactly one field:

```json
{"assertion":"COMPACT_PROVIDER_PRELINK_JWT"}
```

Body limit is 24 KiB; the verifier independently limits the compact proof to
16 KiB. Duplicate or unknown fields, case aliases, another JSON value, missing
assertion and caller-selected IAM targets are rejected. After authentication,
socket reads have an eight-second deadline and socket writes a nine-second
deadline. The request context has the same eight-second bound and is further
limited to the verified assertion's expiry for DB/IAM work. Individual IAM/JWKS
HTTP operations retain their own existing bounds.

Success is HTTP 200 with `state=completed` and these matching-receipt fields:
`operation_id`, `assertion_sha256`, `issuer`, `assertion_jti`, `bot_id`,
`telegram_user_id`, `external_subject`, `user_id`, `subject`, `organization`,
`provider_id`, `expires_at`. Bot/numeric Telegram IDs are JSON integers within
Telegram's safe range. Hash is lowercase hexadecimal SHA-256 of the exact compact
bytes. UUIDs and absolute expiry are serialized as strings. No IAM credential is
included. The provider hook must match the complete tuple/digest and fresh
expiry before relay; receiving HTTP 200 alone is insufficient.

Missing/wrong service credentials return 401; malformed JSON returns 400;
unsupported media returns 415; identity/proof/revocation denial returns 403;
immutable-intent or identity conflict returns 409; restart/unavailable dependencies
return 503. Errors are generic and contain no submitted proof or key. A failed or
lost response is not permission to relay or to invent success.

## Durable reconciliation

After successful signature/scope verification, reserve immutable intent and its
canonical user UUID in a short DB transaction. All IAM HTTP calls run after that
transaction releases its connection. Bot/org/provider are server-selected. If a
local IAM binding exists, use that exact subject; never replace it by selecting
another account with the same synthetic email. Otherwise call EnsureUser with the
persisted local UUID, preserving lost-create reconciliation.

Check proof freshness before IAM work. EnsureTelegramIDPLink freshly validates the
exact active verified-human account and complete exact link ownership; a write
status, conflict or cached receipt is insufficient. Final DB completion rechecks
local revocation, expiry and unchanged binding, then atomically binds/completes.
Completed receipt replay still performs fresh IAM/link verification.

Retries keep the identical compact assertion while fresh. A changed byte digest
under the same issuer/JTI conflicts. A pending receipt survives dependency outage
or recreated API instance. Expired proof cannot resume an operation; a newly
verified assertion with a new JTI may reconcile the same canonical account/link.
Remote IAM effects and local revocation are not one atomic transaction: a link
may exist after a racing local revoke, while final completion denies success.
No automatic unlink, merge or remote-role revocation is introduced.

## Behavioral verification and evidence

Existing event-context
schema-test lint findings were fixed by extracting schema setup, formatting and
making independent subtests parallel; assertions and fixed event time remain.

A separate one-slot test fixture also restarted the full task engine,
whose persistent instance-lock connection exhausted that pool before the intended
HTTP scenario.
The separate [task startup guard](shuttle-task-pool-startup.md) now rejects
a one-connection task-engine configuration before taking an instance lease.
This
endpoint-only pool test does not claim the whole engine can run on one connection.
Source and pg_stat_activity identified the fixture deadlock; the
exact owned runner was explicitly stopped, ending 137 with OOM false.
These logs
remain failed evidence, not accepted results.

The one-slot
check runs the endpoint with a stopped intake service, leaving its sole connection
available for the business transaction under test.

The only skip is the command package's
`TestWatchdogHelper`, an intended child-process entrypoint; integration has no
skips.
Covered paths include all three initial entry orders, simultaneous first
login, lost IAM write and committed HTTP replies, recreated API recovery, IAM
outage with pending intent, pending/completed local revoke, changed digest,
foreign opaque ownership and real one-connection DB availability during IAM HTTP.
This is scoped regression evidence, not a rerun of the complete integration suite.

The synchronized concurrency/outage fixture also passes.
Final IAM session-cookie relay,
real Telegram Web Login credentials and CA trust/browser navigation remain
separate interactive gates.
No production deploy, commit or push occurred.
