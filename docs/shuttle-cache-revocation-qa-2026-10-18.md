# Local revocation with warm IAM authorization caches

Accepted on 2026-10-18 on a fresh isolated real Zitadel/Loom/mock-Telegram stand.
Local user revocation evicted an unexpired positive authorization cache entry,
kept an unrelated administrator usable during IAM downtime, and blocked delivery
of the original agent result. No product code changed. The primary remained on
the separately accepted 46a9 candidate and was preserved throughout.

## Behavioral checks on current sources

Four focused integration tests passed on Windows Go 1.27.2 with real PostgreSQL 17:

- `TestAgentUserRevocationRequiresRoleAndExactApprovalAndRecoversAfterRestart`
- `TestAgentUserRevocationLostCommitReplyInvalidatesCachesAndReconcilesOnce`
- `TestOperatorRevocationLostCommitReplyInvalidatesBothIAMCaches`
- `TestAgentUserRevocationReplayInvalidatesOriginalAndCurrentScopedSubjects`

These tests use mock IAM and actual PostgreSQL transactions, including a dropped
COMMIT response. They check issued-token and positive-cache invalidation through
the shared client, role/exact-parameter approval, original receipt recovery and
subject rebinding. The disposable PostgreSQL container was removed after exit 0.
This was not a race run or a full suite; unchanged product code was not relinted
or rebuilt. Relevant source hashes and the JSON test stream are retained.

## Real-service sequence

The fresh `cache-revocation` stand used the exact bot image
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`,
frozen Loom
`sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7`,
real Zitadel and private TLS manual-provider/mock-Telegram fixtures. All nine
runtime IDs/images/process epochs and 26 source/config/evidence paths were
pinned. IAM/mock loopback mappings and mock/provider TLS endpoints were checked.
Two fresh correctness/safety reviewers were clean after tightening the conservative
TTL bound and exact settings restoration.

1. Create two users through signed synthetic Telegram Mini App bootstrap. Grant
   only the fixture administrator `zinusya.admin` in this stand's API project;
   no real-account or primary-IAM grant is involved. Obtain genuine delegated
   JWTs and set positive-cache TTL 180 through the authenticated settings API.
2. Read the target and prepare one exact-parameter revocation with a durable
   operation ID, original arguments and proposal ID. Submit synthetic update 7801
   as the target through normal intake. Observe one task/run/session and one
   unanswered real Loom model request; the bot issued its delegated credential.
3. Record database time before the first target JWT profile request and observe
   its cache miss. Repeated target/administrator reads then produce two hits
   with no misses. Both JWTs remain valid beyond the 180-second cache window.
4. Stop only the owned IAM container. The same target profile and administrator
   settings reads still return 200 as cache hits. Confirm the original revocation
   through the authenticated agent API using its exact proposal and operation
   key. Observe one canonical revocation audit.
5. The same target JWT now returns 503 with exactly one positive-cache miss and
   no hit. The separate administrator still returns 200 with a cache hit and no
   miss. The target probe completes 2.617105 seconds after the conservative
   pre-insertion timestamp, below TTL 180, with its JWT still valid for more
   than 30 seconds. This distinguishes targeted eviction from natural expiry,
   global cache reset or an ordinary database denial. JWT signature/trust
   validation precedes that cache lookup.
6. The original pending target task becomes `failed` locally. Exactly one
   outbox/message is delivered, with its `:denied:` key. Access-denied notices
   contain no run output and remain permitted during revocation. No business
   operation, replacement run/session or provider response was submitted.
7. Start the same IAM container. The same target JWT now returns 403, confirming
   local account denial after remote identity verification becomes available.
   Reconcile the original revocation receipt; it matches and there is still
   exactly one audit. The bot, Loom, provider and five other services retain
   their original process epochs.
8. Supply one manual response to the original Loom request containing a distinct
   stale-result marker. That original run completes, but the local task remains
   failed and retains only its original denial outbox/message. The stale agent
   result is never delivered. There is still one logical model request and
   zero Loom business write operations.
9. Require the current settings to equal the saved fixture version/document,
   then restore the original document through CAS. Read it back separately.
   Settings versions advance 1→2→3; the old version is not written back.

The decisive counters were hits 12/misses 7 before the target probe,
hits 12/misses 8 after its 503, then hits 13/misses 8 after the administrator control.
Only IAM had an intentional stop/start; the other eight processes were continuous.

## Evidence boundaries

The running bot proves positive-cache eviction, local denial, receipt deduplication
and suppression of stale agent output with genuine IAM tokens. It does not expose
a direct read of its issued-credential cache after revocation: the local revoked
check correctly stops the worker before another `UserToken` call. Accordingly,
`issued_cache_real_eviction_proved` is explicitly false. Direct observation that
the revocation API evicts both caches comes from the four component tests above,
with mock IAM and real PostgreSQL, not from a fabricated real-service claim.

Bootstrap/initData, Telegram transport and the model answer are synthetic/manual.
No real Telegram message, new model-provider implementation, browser CA test,
production action or Loom developer-stand change occurred. External IAM role/token
revocation during a permitted positive-cache window is a separate policy boundary;
this gate tests local revocation. It does not close the
[abrupt Loom death gap L04](shuttle-loom-abrupt-exit-qa-2026-10-18.md).

## Preservation and cleanup

All 112 primary fingerprints matched except River leader bookkeeping. Canonical
queue fields and all 46 full River job rows were identical. Primary bot/poller,
eight companions, independent provider and 132 Loom runs were unchanged.

The temporary stand's containers, volumes and networks are absent. The unit-test
PostgreSQL was also removed. Private credentials, test logs, source hashes,
review gate, cache observations, task/run/outbox receipts, final logs and archives
remain ignored under `.cache/shuttle-work/cache-revocation-v1-qa`.
Application archive: 299723 bytes, SHA-256
`e4d5e430284ef10a91aef5d512ea7e096a3b29d936fecafa80e7cf4022a1848f`.
Loom archive: 90313 bytes, SHA-256
`47eeeedd7de99d21e6367b72c1d24a506e9d54973700445f93e78cec2cb6500c`.
Nothing was staged, committed, pushed or deployed to production.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
