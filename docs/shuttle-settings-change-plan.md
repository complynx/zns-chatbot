# Agent runtime settings change

Next bounded slice: expose the existing application-wide settings replacement to
an authorized agent with durable exact approval and original receipts. Startup
YAML, environment variables and secrets remain outside this API. Settings are one
application-wide singleton, not separate per-event or per-organization documents.

Architecture and Go design consultations agreed on typed arguments
`expected_version` and the full existing `Settings` document. Omitted optional
fields and explicit zero preserve current stored default semantics. Preparation
must show both stored and effective values where defaults apply, scope, observed
version and application timing. Worker concurrency still applies after restart;
the reserved question timeout does not change current Loom prompt expiry.

Migration 29 will add dedicated deployment/actor/operation-scoped settings
proposals and results with a random proposal UUID, normalized document hash,
original preparation and five-minute database expiry. Repeating preparation
returns its original expiry. Exact confirmation checks proposal, hash, current
settings version and active administrator. It commits settings, existing audit
and agent result in one transaction. Replay reads its own result before stale
version/expiry checks and never adopts an operator audit as an agent receipt.
Rollback must retain proposal/result audit, following migration 28's guard.

First extract shared validation and the existing CAS/audit transaction body while
preserving the operator endpoint. A narrow common administrative operation/active
actor lock helper can be shared with task closure; no generic proposal engine is
needed. Keep the existing database CAS as the concurrency authority.

The API must coordinate settings writes with its existing process-local
`settingsMu`. A durable result says which document this operation committed; it
does not prove every running process applied it. After an agent commit or receipt
recovery, read the current database settings and synchronize this process's IAM
TTL. Never install a historical receipt's TTL after a newer settings change.
Track successfully applied TTL locally so status polling does not repeatedly
flush the IAM cache. Startup already reads the persisted TTL.

For the new agent path, an ambiguous commit or failed synchronization remains a
recoverable 503 under the same operation. If current settings cannot be read,
temporarily disable this process's positive IAM cache (TTL zero), clear the local
applied-value marker and reconcile current settings on recovery. This conservative
failure policy is explicitly part of the new agent path; do not silently change
the operator endpoint's failure behavior. A known committed receipt must remain
recoverable even if runtime synchronization initially fails. Do not claim
cross-process cache coherence or atomically coordinated IAM changes.

Behavioral acceptance covers no mutation during preparation; exact argument and
proposal binding; expiry/stale versions; simultaneous confirmation and an
operator/agent CAS race; one audit/result/version advance; rollback on result-save
failure; lost response and ambiguous commit reconciliation; actor/deployment
isolation and local revocation; optional defaults; original receipt after a newer
operator change without restoring old TTL; and repeated status without cache
churn.
Preserve operator and startup behavior.

The first preserving extraction is implemented: validation is separate, and the
existing CAS/audit body can share a caller-owned transaction.
The proposal store, migration 29, agent routes/discovery and process-local
runtime reconciliation are now implemented.
No production rollout is implied.
