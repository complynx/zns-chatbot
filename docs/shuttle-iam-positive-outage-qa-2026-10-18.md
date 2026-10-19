# IAM positive-cache outage QA — 2026-10-18

Status: PASS for the installed bot's inbound API positive-introspection cache
against actual stopped/restarted Zitadel. Owned `shuttle-live-test`, schema48 /
`mvp-55` only. No production or Loom-developer stand changed. This gate does not
accept bot token issuance/renewal fallback, JWT expiry, Loom auth caching or
combined downtime after a run already exists.

## Controlled fixture and restoration

Initial runtime settings were version 5 with `auth_cache_ttl_seconds=0`. The
operator froze all ten Compose container IDs/images/settings, the counted
provider, bound configuration bytes, database identity/history and 109 atomic
PostgreSQL table/sequence/schema fingerprints. All bot tasks/outbox were settled.

The same ingress and bot containers stopped normally. While both were stopped,
an exact-old-row guarded SQL fixture changed only the TTL to 60 and version to 6.
This is isolated test setup, not an accepted administrator settings API workflow.
No settings audit or business action was created. The same bot started and loaded
the fixture. Its private API alone received synthetic read requests.

Two distinct, newly issued user JWTs stayed in process memory. Their expiries
were more than two minutes away; neither JWT expiry nor token corruption caused
the expected failures. The first token warmed signature keys and positive
introspection. The second token was never presented to the bot before the outage.
No account was created, revoked or granted any permission.

## Observed requests and metrics

| Condition | Result | Duration |
| --- | --- | --- |
| First token, IAM available, first verification |200|46ms|
| Same token, IAM available, positive cache |200|16ms|
| Same token, exact own IAM stopped |200|31ms|
| Distinct never-warmed valid token, IAM stopped |503|3.000s|
| First valid token after its 60-second cache TTL, IAM stopped |503|3.000s|
| Newly acquired token after full restoration |200|63ms|

These are individual observations, not latency guarantees. All successful
profile fields matched after excluding their query-time `observed_at`.

Private cache counters were hit 1/miss 1 before IAM stopped, hit 2/miss 2 after the
warm/cold outage reads and hit 2/miss 3 after TTL expiry. The expired cache entry
was not used as authorization. JWT trust validation precedes these counters: the expiry probe incremented a
cache miss and returned 503 rather than a JWT denial. The source JWTs still had
time remaining.
Service/user credential fallback counters are not this gate's acceptance signal.
No response body, JWT or private profile value appears in this report.

## Recovery and preserved state

The finally path started the exact original IAM, stopped the same bot, restored
the complete original settings row and version 5, and started that same bot and
poller. It attempted independent restoration before evaluating broader companion
acceptance. A standalone restore command is available for interrupted execution;
it validates each target's identity/configuration and fences settings writes by
the complete expected row and database identity.

Final readiness and a new IAM exchange plus authenticated API read passed.
Current positive cache TTL is again 0. After the final bot restart the counters
were hit 0/miss 1. The last measured bot stop was 0.953 seconds; the driver records
only the latest stop per service, so no claim is made about a retained first-stop
duration. Zitadel stopped in 1.813 seconds, ingress in 0.719 seconds. All stop checks
required normal exit 0, not forced termination.

All durable business, user, task, outbox, receipt and settings rows matched the
original fingerprints, as did schema/history and sequences. The only observed
deltas were River leader and queue bookkeeping. The seven nontarget Compose
services and counted provider retained their exact fingerprints. The three
targets retained container/image/configuration identities; their start times
changed. All ten Compose services run without OOM, and no task is unfinished.
No extra container, volume or builder was created.

## Review and evidence

An independent review found that an unrelated companion change could prevent
recovery before any stopped service was started. The corrected operator restores
independently verified targets before broader acceptance. Four offline behavioral
checks pass: probe exception dispatches restoration, unrelated companion failure
does not prevent recovery, IAM start failure does not skip other targets, and a
foreign settings row is preserved despite a stale success flag.

A fresh strict textual review returned no actionable findings. An intermediate
review used a read-only shell command to load its diff against the requested
review protocol; it was superseded by the fresh strict review. No runtime/test
execution was delegated to reviewers. The actual outage attempt passed without
retrying a business operation. The index remained empty.

Ignored evidence: `.cache/shuttle-work/iam-positive-outage-v1-qa/checkpoint.json`,
its 109 before/after fingerprints, exact settings snapshot, status/metric/duration
receipts and new container baseline. Offline checks retain source hashes and log
under `.cache/shuttle-work/iam-positive-outage-driver-tests-v1/`. Tokens are not
persisted. Older metrics QA failures and older service-outage gates remain intact.

Next: bot service/user credential cache and renewal boundaries, real JWT expiry,
and combined IAM/Loom downtime after an existing run. Preserve the untyped
`module_discovery_unavailable` denial/transient boundary; do not blindly resume
uncertain provider/tool execution. Native reviewer UI confirmation remains
unaccepted for its separately recorded browser-tool barrier.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
