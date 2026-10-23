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
