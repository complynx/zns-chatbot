# Local pilot acceptance, 2026-10-23

Status: bounded local integration, restart/rollback and archive/cleanup pass.
This stand used fresh synthetic identities and databases. It did not change
production; its owned containers, volumes and networks have been removed.

## Exact runtime and isolation

The application is source `ff8d4234`, local image `87103b826849`. Its previous
binary is image `a2fae485515d`. IAM4.20 uses image `9ea0e9ecfdc4`; Loom uses
API `cbba225f8491` / source `ba1f4a11` and worker `856d5968d9d0`.
The actual published ZitadelTG image is `8864dd802d83`; its source revision is
not asserted because the image has no revision metadata. PostgreSQL17,
Traefik3.6.25 and Python3.14 are pinned by digest in the local receipt.

The owned project is `zinusya-pilot-local-20261023`. Backend services use an
internal Docker network; only the gateway has an operator bridge. Host ingress
binds loopback. A private CA and explicit TLS server names are verified normally;
there is no certificate-verification bypass. Secrets, assertions, sessions and
database archives remain in the protected local directory.

Fresh IAM projects, introspection clients, token exchange, caller grant and
provider configuration were provisioned once. The runtime service actor receives
`ORG_USER_MANAGER` and `ORG_END_USER_IMPERSONATOR`, without `ORG_OWNER`.
PostgreSQL migration and runtime roles are separate. The runtime role is not
superuser, cannot create databases/roles or objects in the public schema, and is
not a member of the migrator role. TEMPORARY remains granted. Both application
and Loom migrations completed.

## Observed integration

The actual published provider handled its login/callback, validated a locally
signed Telegram identity with nonce/state, signed and dispatched the prelink
hook, and received the controlled browser relay. The callback returned303;
the application prelink receipt completed. A wrong hook credential returned401
without creating a user. Native Mini App HMAC bootstrap and authenticated
`/app/me` read resolved to that same canonical application user.

Real scoped IAM actor/user tokens and the existing Loom authentication session
carried the owner identity into the actual Loom loop. Discovery exposed the
application module; a controlled script response read `zinusya_account.me`.
The next provider request contained the authenticated owner's actual result,
and the run completed after the second controlled response. A direct delegated
account read matched it. No application task or outbox item was created.

Private metrics expose the expected queue/task/outbox/database families.
Public metrics and internal/prelink paths returned404. The local OTLP receiver
observed the supplied trace ID from the authenticated request. The application
then restarted gracefully and accepted the same native session for the same
owner.

One fresh native session subsequently passed authenticated owner reads on
current image `87103b82`, previous image `a2fae485`, and restored current image.
The owner table hash stayed equal across this round trip. Actual Docker image,
UID10001, running state and resource ownership were checked. Public routes were
observably fenced before each binary transition and restored afterward.
The operator gateway briefly restarted when applying each route mode; its
operator paths recovered and were verified. Continuous operator availability
is not claimed.

## Retained failures and narrow continuations

Original one-shot failures and protected journals remain unchanged. Separately
reviewed continuations use exact source hashes and persisted effects; completed
or uncertain bootstrap, provisioning and identity writes were not replayed.

- Docker Desktop did not publish ports for the internal-only backend network.
  The gateway alone received an operator bridge; PostgreSQL and IAM were retained.
- The helper filename `operator.py` shadowed Python's standard library. A
  separately reviewed isolated launcher preloads the real module.
- Traefik rejected the dynamic `.json` filename. The same valid YAML-compatible
  bytes were mounted under `.yaml` and only the gateway was recreated.
- A placeholder guard rejected comment-only examples. Offline checks were
  corrected; active values and actual application config loading still pass.
- A Windows key bind appeared as0777 to the published provider. The same signing
  key was copied to an owned Linux volume as0600/UID10001; no key was regenerated.
- Restricted discovery inspection timed out for the app with an unused host
  binding. Removing only that binding changed its measured result from503 at
  3023ms to200 at25ms; the app retained its image, config and database.
- The probe expected200 for a successfully created Loom session201. The exact
  preserved response and scoped live database count verified that existing
  session. A continuation adopted it without repeating the session POST and
  used the documented202 for the first asynchronous run.
- The route-fence file changed on both host and gateway, but live routing
  remained stale despite file watching. The original fence check failed before
  starting the old binary. Explicit gateway restart applied the fence and
  recovered operator routes. The filesystem-notification cause remains likely;
  uninterrupted operator transport is not accepted.
- The first old-binary owner read returned401 after the native session's natural
  expiry,54 seconds before that read. It does not establish incompatibility.
  The separately reviewed fresh-session round trip passed, with the normal
  900-second TTL unchanged and the expired original receipt retained. An invalid
  proposed remaining-lifetime assertion was caught during review before effects;
  the corrected guard requires600 seconds remaining after bootstrap.

## Archive and cleanup

Custom PostgreSQL archives for `loom`, `pilot` and `zitadel` were saved privately
and fully decoded through `pg_restore --file=-` before removing owned resources.
Decoded outputs contain79169,342189 and670477 bytes respectively. This is full
archive decode, not a claim that a restored database was exercised.
Cleanup completed with no unknown effect or pending cleanup. A separate root
read-only Docker observation found zero containers, zero volumes and zero
networks for this exact project, including the temporary Linux signing-key copy.
Protected original sources, failed journals, session receipts and backups remain.

## Evidence and limits

Local evidence resides under
`.cache/shuttle-work/parallel-acceptance-20261022/pilot-local-20261023/`.
`runtime-continuation.json` records runtime readiness;
`probe-continuation.json` records the actual integration and telemetry probe.
`acceptance.json` and `fresh-session-continuation.json` record the successful
fresh-session binary round trip; `cleanup.json` records all three full decodes.
Private raw responses are not published with this report.

Telegram issuer/JWKS and the post-hook browser relay are controlled local peers.
This does not establish real Telegram login, full human OIDC/WebView behavior
or delivery. The two Loom responses are explicitly controlled fixtures, with
zero paid generations and zero Telegram sends. Target-model behavior is covered
separately by the [real-model voice campaign](shuttle-persona-model-qa-2026-10-23.md).
Local OTLP receipt is not Grafana Cloud ingestion, alert installation or contact
delivery. No production copy was used and no live financial operation was run.

Merge, final image publication, production provisioning/deployment/cutover and
human Telegram testing retain their separate authorization requirements.
Accepted unchanged IAM, import and business evidence is reused.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
