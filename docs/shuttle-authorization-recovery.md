# Loom authorization recovery

Shuttle keeps the accepted task, session, run ID, immutable request and operation
key when Loom definitively rejects a request with 401. It records a five-minute
retry deadline in the durable checkpoint and waits through the normal queue.
Restart does not renew that deadline. Before a run creation has been prepared,
continuous 401 beyond the deadline produces an access-denied result. Once creation
is prepared or a run ID is known, the task retains ownership and waits for access
to recover even beyond that deadline. A lost creation response can hide an active
run with `disconnect_policy: continue`; an authorization timeout does not prove
it stopped. New messages keep routing to the retained task. Other definitive
denials such as 403 remain terminal.

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
leaves `waiting_auth`. An existing run can still renew and recover after that
deadline; expiring it must not release conversation ownership.
Authorization-required is not proof of a temporary failure:
Loom may use it for other authorization errors. Fresh IAM and Loom checks remain
authoritative. Explicit `permission_revoked` ends with denial and no renewal.
Other waiting-auth reasons remain waiting without automatic renewal. Dependency
outages keep the existing policy above: they do not consume a continuous-denial
budget. Local revocation is checked before recovery work.
