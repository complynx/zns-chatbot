# Current identity client expiry on IAM 4.20, 2026-10-22

The current Go identity client passed natural credential/JWT expiry and recovery
against the retained, isolated Zitadel 4.20 clone from the
[current prelink gate](shuttle-prelink420-current-qa-2026-10-22.md). This is client
integration evidence. The bot, provider, Loom and Telegram were not running in
this gate; it does not accept running-task session renewal on the new IAM build.

## Scope and execution

The helper was built with Go 1.27.2, Linux/amd64 and CGO disabled, importing the
repository's current `internal/identity` implementation. The reviewed manifest
bound source/module files, helper source and executable, fixture configuration,
CA and retained receipt. Before starting, the controller checked stopped state,
ownership and exact IAM/bot image IDs against that receipt. The IAM image was
`sha256:9ea0e9ecfdc43115da871242e40a0320f2ece864c7cd8b5d41a863a7f4902499`.
No application source was changed for this acceptance.

Only the existing PostgreSQL, TLS gateway and IAM containers were started. The
helper used the existing synthetic account and real `ServiceToken`, `UserToken`,
`Verify` and `CacheStats` methods. No cache value or system clock was edited.
TLS verification remained enabled. Tokens stayed in helper memory; evidence
contains SHA256 digests, expiry, synthetic subject/actor and error classifications.

All four original OIDC settings were captured. Only this clone's access/id-token
lifetimes were temporarily set to 90 seconds; refresh settings were preserved.
Settings were read back before issuance and restored/read back during cleanup.
The helper used user `10001:10001`, a read-only root, dropped capabilities, no new
privileges, one CPU, 96 MiB RAM/total swap and 32 PIDs. It had a 240-second context.

## Accepted behavior

1. Real service and delegated user credentials were issued. `Verify` confirmed
   the expected canonical subject and machine actor.
2. After only the same owned IAM container stopped, the warm client returned the
   identical credentials without fallback. A new cold client returned
   `ErrUnavailable` for both service and user issuance.
3. In the renewal window, with IAM still stopped, the warm client returned the
   same still-valid credentials through fallback. Counters recorded two service
   fallbacks and one user fallback; delegation also requested its actor token.
4. The probe waited beyond the maximum measured credential, verified principal
   and signed JWT expiry. Service/user issuance then returned `ErrUnavailable`;
   expired credentials were not reused. At `18:54:49.702Z`, `Verify` returned
   `ErrDenied` for the original JWT whose measured `exp` was `18:54:45Z`.
5. After that same IAM container restarted and its authenticated Management API
   became ready, the same client obtained new service/user token digests with
   later expiry. `Verify` again confirmed the original subject and actor.

Application user, prelink and task table hashes were unchanged. The helper exited
0 without OOM and was removed. All six retained fixture containers ended stopped
with exit 0 and no OOM; the three unrelated containers were never started.
No real Telegram authorization, model call or production change occurred.

## Failed attempt, diagnosis and review

The first complete probe remains failed: service/user issuance refused expired
cached credentials, but `Verify` did not return the expected `ErrDenied`. That
probe did not record the JWT's actual `exp` or error classification, so its exact
failure mechanism is unproven. Its original settings were restored, helper
removed and all containers stopped; it was not relabelled accepted.

A separate warm-only diagnosis measured an OAuth credential expiry slightly
earlier than the signed JWT and verified principal expiry. The fresh accepted
probe recorded both values and waited beyond their maximum; it also recorded the
actual verification classification before retaining the strict `ErrDenied`
assertion. This establishes the accepted run, not a retrospective diagnosis of
the failed one. No product fix was inferred from the incomplete original record.

Independent reviews fixed cleanup coupling, protection of source-fixture failure
logs and exact image binding before execution. Helper diagnostic/removal failure
cannot bypass a settings-restoration attempt, and each service stop is attempted
independently. Fresh final reviews found no actionable findings. The initial
sandboxed build failed on filesystem access; the separate offline elevated build
succeeded. No runtime effect occurred during that build failure.

Ignored evidence is under
`.cache/shuttle-work/credential420-current-20261022-v1/`: original failed receipt,
`diagnosis/` warm measurements and `r2/` accepted receipt, expiry observation,
source/review manifest, helper log/inspection and cleanup receipt. These one-shot
controllers must not be replayed to read results. Current bot-task/Loom renewal,
simultaneous service outages, revocation and real Telegram remain separate from
this client-only gate; earlier versions have their own dated acceptance reports.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
