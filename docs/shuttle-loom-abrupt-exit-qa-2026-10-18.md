# L04: delayed execution recovery after abrupt Loom death

The 2026-10-18 SIGKILL gate did not pass automatic task recovery. A fresh owned
real IAM/Loom/mock-Telegram stand retained its original task, run, session and
unanswered model request, but the run had no live lease and no new execution
117.112 seconds after the same Loom container was started again. The bot, provider,
IAM and other services continued without restarting. This is a bounded runtime
observation, not proof that recovery never happens.

Status: published as [Loom issue #19](https://github.com/complynx/loom/issues/19)
on 2026-10-21. At the time of the original probe no authenticated write channel
was available; the later publication used `gh`. The Loom maintainer can classify
this as a bug or intended recovery policy; the integration requirement remains
unresolved. Publication does not turn the historical observation into a new
runtime test of the current Loom version.

## Later published-runtime acceptance

Published Loom `34f46468` subsequently passes the separate
[actual-IAM pending-inference crash gate](shuttle-loom-abrupt-recovery-qa-2026-10-22.md).
It preserves original task/run/session identity and unknown inference exposure,
then delivers one final response. That bounded result does not rewrite this
older failed observation or establish recovery guarantees for every crash window.

## Reproduction and observations

The stand used bot image
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`,
Loom image
`sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7`,
real Zitadel, private manual-provider TLS and mock Telegram. It preserved the
ordinary 600-second active-run setting, 15-second run lease and two-sponsored-
attempt limit. It did not shorten queue recovery or change database clocks.

1. Submit synthetic update 7701 through ordinary intake. Observe one user,
   inbox/task, delegated session, running Loom run with a live lease, one pending
   model step/request, and one running `clx_execute_run` River job at attempt 1.
2. Send `SIGKILL` to only that owned Loom container. Observe exit 137 without OOM;
   all eight other container/process epochs remain unchanged.
3. Start the same container, observe readiness and keep the same bot process.
   The restart was recorded at 12:26:14.238335Z. No manual Resume, operator recovery,
   SQL state change, new message or model response was supplied.
4. At12:28:11.350445Z, the original run was still `running` with an empty reason;
   its lease had expired at 12:26:21.965572Z. Its original River job was still
   `running`, attempt 1, attempted at 12:25:57.750876Z. The original model step was
   pending, there was still only one logical request and zero sponsored-attempt
   rows. No replacement run/session/task, business write, outbox or Telegram
   attempt/message existed.

The driver records `passed=false`, `phase=recovery_gap_observed`. It never
converts that timeout into success or manually pushes the task onward. The
fixture was archived and retired after recording the live state. Any database
state changes caused by later cleanup are separate from the pre-cleanup snapshot.

## Source explanation and its limits

Source inspection at Loom commit
[`fc6c5747a4359ff94a58071054e124ba1449cc89`](https://github.com/complynx/loom/tree/fc6c5747a4359ff94a58071054e124ba1449cc89)
shows two different recovery clocks:

- `internal/app/runtime.go`, `runtimeQueue`, sets River `JobTimeout` from
  `RunActiveTimeSeconds` but does not configure `RescueStuckJobsAfter`.
- River v0.49.0 `client.go` default calculation adds its one-hour rescue default
  to a positive JobTimeout when no explicit rescue threshold is set. For 600
  seconds, that computes 4200 seconds, approximately 70 minutes. This is a source
  inference about eligibility, not a 70-minute runtime measurement or an exact
  eventual-recovery guarantee.
- `internal/runs/provider_recovery.go` invokes `RecoverSponsoredRun` before an
  execution claim. `internal/store/platform_attempts.go` can fence expired
  inference and account for the unknown exposure after fresh authorization.
  It needs a worker invocation. The observed orphan River job had not supplied
  one within this window.

The pinned image and the source commit are recorded separately; the image has
no `org.opencontainers.image.revision` label. Source analysis supplements the
runtime evidence and does not turn it into a new build/provenance attestation.
A graceful Loom shutdown is a different path: the existing accepted
[shutdown-resume behavior](shuttle-loom-resume.md) observes `paused/shutdown`.
The killed run stayed `running`, so Zinusya correctly did not call that Resume
path or create a replacement run.

## Requested upstream recovery contract

Please provide bounded automatic recovery for an execution orphaned by process
death, preserving these requirements. The implementation and target recovery
interval are for the Loom maintainer to choose; do not lower a global queue
threshold so far that active work is dispatched twice.

1. After the run lease expires, atomically fence the old executor and arrange
   durable queue delivery for the original run. A still-valid lease, client
   pause/cancel, uncertain non-model effect or exhausted recovery budget must
   retain its existing protections.
2. Recheck service/caller and user authorization before another inference or
   settlement. If permitted, preserve the original inference as unknown with
   its exposure accounting and use a new logical model request under the
   configured sponsored-attempt limit. Do not overwrite the lost attempt or
   charge it as a successful user response.
3. Preserve task/run/session identity and all business-operation deduplication.
   Do not require the caller to invoke an administrator endpoint that assumes
   every execution worker has stopped. Zinusya must not bypass reconciliation
   with an arbitrary Resume, synthetic receipt or replacement run.
4. Add behavioral crash acceptance: real SIGKILL during pending inference,
   continuously running bot/provider/IAM, original durable work restored after
   the lease expires, one final result and retained unknown accounting. Cover
   live-lease rejection, competing recovery claims, revoked authorization and
   uncertain business effects as critical paths.

The existing 110-second QA observation threshold is a diagnostic limit, not an
agreed production SLA. We can change it when the explicit recovery policy and
contract are available. Zinusya's full abrupt-exit acceptance remains open.

## Review, preservation and cleanup

The driver passed fresh independent correctness and safety reviews. Fifteen
source/config hashes and all nine owned runtime IDs/images/epochs were pinned;
IAM/mock loopback port and private provider bindings were checked. Manual
inference is supported by the positive branch but was never supplied in this
observed failure. No product source or Loom developer checkout/stand was changed.

All 112 primary fingerprints matched except River leader/queue bookkeeping;
canonical queue metadata/pause fields and all 46 full River job rows matched.
Primary bot/poller, eight companions/provider and 132 Loom runs were unchanged.
The primary remains on the separately accepted 46a9 candidate.

The temporary project's containers, volumes and networks are all absent. Private
source analysis, observations, pre-cleanup state, logs, review gate and database
archives are retained under `.cache/shuttle-work/abrupt-loom-v1-qa`.
Application archive: 298360 bytes, SHA-256
`0d7aeff431969238b937b10f9ca33037fdde0b1a3d34f9fb691fc0df6be5cc58`.
Loom archive: 89790 bytes, SHA-256
`613a3092ca7992c62767767fa14564dfe29ed02119a26cbe3db4285d8a10f323`.
No production, publication or real Telegram action occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
