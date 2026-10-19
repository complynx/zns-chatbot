# Loom authorization recovery

Loom authorizes service callers through fresh Docker-discovered manifests.
Stopping Shuttle removes its caller registration; its next discovery refresh
restores that registration after restart. With actual IAM and the same persisted
Loom auth session/run, the local stand returned HTTP 200, then 401 while Shuttle
was stopped, then 200 after it returned. Credentials and user permissions were
not changed. An opaque 401 therefore cannot always be treated as final denial.

Shuttle keeps the accepted task, session, run ID, immutable request and operation
key when Loom definitively rejects a request with 401. It records a five-minute
retry deadline in the durable checkpoint and waits through the normal queue.
Restart does not renew that deadline. Continuous 401 beyond the deadline produces
an access-denied result. Other definitive denials such as 403 remain terminal.

A successful run response outside `waiting_auth` clears the grace deadline. Transient IAM/Loom transport
outages also clear it: dependency downtime does not consume the continuous-denial
budget. Local revocation is checked before subsequent calls. Loom still validates
credentials/permissions on every attempt; waiting never grants access.

Recovery preserves a still-valid auth session rather than forcing a mutating
reauthorization on each 401. Expiry-driven token/session renewal remains active.
A lost reauthorization receipt still becomes auth_unknown and is not blindly
replayed. Controls keep their original request/key and preserve earlier unknown
effects even if a later attempt is rejected.

A successful HTTP response can separately report `waiting_auth` with reason
`authorization_required`. Polling alone does not restart this run. Shuttle saves
the observed run ID and authorization wait, invalidates the cached subject token,
then reauthorizes the same session with its current version even if it has not
expired. Loom atomically queues that session's waiting runs. No Resume, replacement
session or new run is needed. The known renewal receipt is saved before a fresh
run read; an uncertain renewal retains the existing `auth_unknown` gate.

The continuous-denial deadline survives successful renewals until a fresh read
leaves `waiting_auth`. Authorization-required is not proof of a temporary failure:
Loom may use it for other authorization errors. Fresh IAM and Loom checks remain
authoritative. Explicit `permission_revoked` ends with denial and no renewal.
Other waiting-auth reasons remain waiting without automatic renewal. Dependency
outages keep the existing policy above: they do not consume a continuous-denial
budget. Local revocation is checked before recovery work.

Behavioral coverage uses real PostgreSQL with HTTP peers: bootstrap denial,
existing-run recovery across two restarts, persistent denial deadline after a
long stopped period, local revocation, availability outage and a lost renewal
receipt. Steering cases cover both definite auth rejection and an earlier unknown
effect. No schema migration is needed; the checkpoint field is optional.

This document describes the recovery behavior. The real-service acceptance
report retains the original failure and records the new image/proof separately.
The later real Telegram restart exposed and then recovered the HTTP-200
waiting-auth case; see [tracing and recovery QA](shuttle-tracing-qa-2026-10-15.md).

## Later actual IAM outage gate, 2026-10-18

The [real IAM positive-cache outage QA](shuttle-iam-positive-outage-qa-2026-10-18.md) runs on schema48 / `mvp-55`.
With a temporary 60-second positive introspection cache, a warmed valid user JWT
returned 200 while Zitadel was stopped. Another never-warmed valid JWT returned 503;
after TTL expiry the first JWT also returned 503. It had not expired cryptographically.
Metrics advanced from hit 1/miss 1 to hit 2/miss 2 and then hit 2/miss 3. Original cache
TTL 0 and settings version 5 were restored; a freshly acquired user token then
returned 200. Business/task/outbox/settings/schema fingerprints matched.

This accepts inbound API positive-introspection caching only. It does not accept
bot token issuance/renewal fallback, expired JWT recovery, cached Loom authorization
or simultaneous existing-run IAM/Loom outages. No Telegram message, IAM permission
change, application-code change or production action was needed.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
