# Identity cache metrics

The private metrics listener exposes two cumulative OTel counters with four fixed
series. They contain no subject, actor, organization, issuer, token, token hash or
caller-supplied label. Counters belong to the local IAM client and reset when the
bot process restarts. An invalidation or TTL change does not reset history.

| Prometheus series | Meaning |
| --- | --- |
| `zinusya_identity_positive_cache_lookups_total{outcome="hit"}` | A trusted JWT lookup used an unexpired positive introspection result. |
| `zinusya_identity_positive_cache_lookups_total{outcome="miss"}` | A trusted JWT lookup found no usable positive introspection result. |
| `zinusya_identity_token_fallbacks_total{kind="service"}` | A singleflight service-token renewal decision returned an unexpired cached credential after a transient failure. |
| `zinusya_identity_token_fallbacks_total{kind="user"}` | The same fallback decision for a user credential. |

JWT trust validation precedes positive lookup counting. Rejection or failure
before that lookup contributes neither a hit nor a miss. A miss includes absent
or expired entries and disabled positive caching; it does not mean introspection
completed or authorization succeeded. Concurrent misses may cause separate
introspection requests. The hit ratio therefore describes this lookup point,
not all incoming authentication requests or token issuance.

Fallback counting occurs inside the token singleflight execution, after its
existing transient-error, expiry and invalidation-generation checks. Several
callers can share one decision. Nested user renewal may use a service fallback
and then a user fallback; those are two decisions, not two end-user requests.
Denial, expiry and a failed invalidation fence do not count as successful fallback.
This is not a counter of all IAM failures, refresh attempts or callers served.

Scrapes copy four numbers under the existing client mutex. They perform no IAM
request, token refresh, cache pruning or authorization change. All four series
are emitted at startup, including zero values. Prometheus collection is owned by
the application's telemetry provider; identity has no telemetry dependency.
Public application routes do not expose these metrics. Existing private-listener
network restrictions still apply.

The slice adds no migration, configuration setting or dependency. Existing cache
policy, generation fences and token lifetimes remain authoritative. Occupancy,
issuance outcomes, latency, JWKS/discovery metrics and dashboards are separate work.

## Verification checkpoint

Architecture and Go consultations agreed on this bounded counter design. Fresh
correctness and quality reviews found no actionable findings; a subsequent fresh
review also covered the final local lookup and startup registration extraction.
Focused Linux race checks passed for identity, observability and application
composition. Final scoped lint reported zero issues after earlier complexity,
formatting and temporary extraction compile errors were corrected.

Behavioral checks cover positive miss/hit, disabled caching and transient failure,
JWT trust rejection, non-counting denied/expired fallback, nested service/user
fallback and concurrent lookup/snapshot reads. Export checks cover all four zero
series, repeated scrapes without IAM traffic and absence of private canaries.
The full current-source race gate passed all 181 enumerated integration scenarios
in 13 bounded groups and all other explicit Go packages. All 15 runner states
recorded exit zero without OOM. The 224-file source freeze stayed unchanged.

## Owned native acceptance, 2026-10-15

Candidate `sha256:1b89ed463ee64f75e41298924e199680782aee00e5004d2db75f3e75b7d2afc2`
was built in a 512 MiB, network-disabled container and deployed only to the owned
stand. Exact image readback passed. No migration was needed: schema 27 / manifest
`mvp-26` remained. Selected hashes for 22 business, task, outbox and receipt tables,
and the existing Mini App origin, matched before and after rollout and restart.
Loom's immutable upstream override remained in the Compose stack.

Real owner-authenticated profile reads produced one positive-cache miss followed
by one hit. Eight concurrent private scrapes preserved those counts and exposed
exactly the four allowed family/label pairs. Public `/metrics` returned 404 with
no identity samples. Bot restart reset all four process-local values to zero.
Service/user fallback counters stayed zero in this native probe; their nonzero
behavior is covered by the behavioral tests, not by a claimed native IAM outage.

Two earlier native verifier failures remain recorded. The first compared entire
profile responses, including the per-query `observed_at`. The second corrected
that comparison but expected a hit while the stand's stored TTL was zero, so the
application correctly reported two misses. Neither attempt is reported as a pass.
The accepted probe compared all profile fields except `observed_at` and used a
separate, explicitly recorded settings fixture: guarded TTL 0 to 30 seconds,
followed by exact restoration of the original settings document and version.
The bot restarted after restoration and readiness passed with the original TTL 0.
These fixture writes were configuration changes on the owned stand; no business
write, Telegram send, production or developer-stand change occurred.

Helper reviews tightened image readback, fixed family/label validation and public
response-body checks before acceptance. Fresh final helper and recovery reviews
found no actionable findings. Original failed checkpoints were left intact.
Ignored evidence includes `native-identity-metrics-checkpoint.json`,
`native-identity-metrics-recovery-checkpoint.json` and the accepted
`native-identity-metrics-enabled-cache-checkpoint.json`, plus build/gate logs and
container states under `.cache/shuttle-work`. The disposable QA PostgreSQL
container and its verified private volume were removed after the full gate;
terminal runners and the build container were also removed.

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

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
