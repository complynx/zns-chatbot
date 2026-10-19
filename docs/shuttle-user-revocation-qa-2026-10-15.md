# Local user revocation acceptance, 2026-10-15

This slice exposes local account revocation through the agent administration API.
It is acceptance on our owned `shuttle-live-test` stand, not production rollout.
The [contract](shuttle-user-revocations.md) defines exact approval, replay and the
separate recovery path after self-revocation.

## Source and regression checks

Architecture and Go consultations preceded implementation. Fresh independent
correctness and quality reviews passed for the transactional extraction, proposal
store and complete API slice. The extraction also fixed existing operator cache
invalidation when PostgreSQL committed but its COMMIT acknowledgment was lost.

All 221 listed integration scenarios and the other explicit Go packages passed
under the race detector. Fifteen bounded integration batches, enumeration and
package checks produced 17 successful container exits without OOM. Strict lint
reported zero issues; pinned sqlc regeneration matched. All 304 frozen source
hashes stayed unchanged through the regression gate, build and native acceptance.
The index remained empty.

Behavioral tests cover exact arguments/proposal matching, expiry after row-lock
waits, scope and role denial, concurrent delivery and reciprocal/self writes,
rollback of revocation/audit/result together, already-revoked fresh auditing,
historical receipt replay after rebinding/restart, and both IAM caches. Real
PostgreSQL protocol tests separately drop actual COMMIT acknowledgments for
preparation, confirmation and self-revocation; native stand tests below do not
repeat that fault injection.

## Owned native stand

The accepted bot image is
`sha256:223fad22c5e00f661fed9ddf5adf410c2273a971c2128e229a088d2d1dabcfa6`,
with schema 30 and manifest `mvp-29`. The prior settings image
`sha256:6e1916fe36be5d0067018e834f40b54b3b2ee66dbb717361022c3ccec6cf6007`
was retained for binary rollback checks. Migration was explicit; ordinary startup
was not treated as migration.

Before migration, the owned PostgreSQL database was backed up and its archive
listing checked. Backup SHA-256:
`edd7060af7fdebce8fd92031e813d2845049b0427d6bdb3fab23bea42fbf57ca`.
This records a backup/listing check and a later audit-data comparison, not a full
database restore rehearsal.

Two new synthetic identities were created in the owned Zitadel and local database:
a roleless target and an administrator with the two required project grants.
The existing fixture administrator remained active for recovery. Existing local
users, including the Telegram account, were not modified. No Telegram messages
were sent in this slice.

Real server-side token exchange and the real Loom broker exercised:

- Roleless discovery returning no administrator methods and an administrative
  read returning HTTP 403.
- `user` through a Loom script, followed by `revoke_user` through exact approval.
  The target was still active with zero audits before approval; afterward it had
  one audit and one durable result. Same-operation replay returned that result.
  Original run: `9fd9bc70-34b4-406a-ac16-28d200388d21`.
- Self-revocation with an explicit warning and exact approval. Loom recorded the
  successful tool result; the account became locally revoked with one audit.
  Owner status, replay and settings calls subsequently returned HTTP 403.
  Original self run: `7be95113-228d-49b6-831b-88663cc73ace`.
- `user_revocation_receipt` through a script under the other active administrator.
  It returned the original actor, operation, proposal and result without taking
  over the write. Diagnostic run:
  `41895b91-3113-4200-9d85-086eaca9f9ce`.

Self-revocation deliberately permits no further bot API access. On Loom 1050,
the next discovery call therefore returned 403 and the owner run paused with
`module_discovery_unavailable`. It produced no final model response. Architecture
review confirmed that successful tool execution plus subsequent denial and
independent receipt recovery is the agreed boundary. The paused fixture run was
then cancelled through the authorized Loom lifecycle API for QA cleanup. Its
successful tool result and original receipt remain retained; cancellation is not
reported as natural model completion. The ordinary and diagnostic runs completed.

## Restart, preservation and cleanup

The previous settings binary ran against retained schema 30. Real-IAM settings
reads and its original agent receipt succeeded. The new image was then restored;
ordinary reconciliation and the other-administrator self receipt still returned
their original results. There are exactly two new committed revocation proposals
and one audit per new target.

The original 22 selected application-table hashes matched after excluding only
the two recorded new users from the pre-existing-users comparison. Four additional
settings/settings-audit/settings-proposal/task-closure-proposal hashes matched.
Pre-existing identity audit rows were also compared with data extracted from the
pre-migration archive. The two new revoked users, audits and proposal results are
retained as history. Current settings remain version 3 with IAM cache TTL zero;
the earlier settings receipt remains version 2 with TTL 120, without replacing
current settings.

Final readback found nine bounded services running without OOM and an empty
owned due-job queue with successful collectors. Temporary build/test containers
were removed; the standalone regression PostgreSQL and its exact disposable
volume had already been removed. No global prune was used.

## Retained failures and acceptance limits

The first image-assembly command incorrectly used a raw image ID in `FROM`.
The binary build itself succeeded. Assembly was corrected through the local base
tag after verifying its exact accepted image ID; the original failure log remains.

The first native harness attempted a read with `module_call`; Loom returned
`use_script_for_reads`. A script completed that read in the same run. A subsequent
harness assertion selected an abandoned completed model-step request. Selection
was corrected to the current pending model step. During these investigations the
manual provider exhausted its retries and paused the original run. An explicit
operator recovery temporarily raised the owned mock provider's sponsored attempt
limit from 2 to 3, resumed the same run and completed it without repeating the
committed write. The exact original configuration was restored and read back.

The harness also initially expected a further model turn after self-revocation.
That expectation conflicted with the required denial of subsequent owner calls;
the corrected boundary is recorded above. Guarded continuation and recovery
helpers passed fresh independent review before execution; original observations
remain retained. An additional
read-only verifier invocation omitted pg_restore's output argument; the corrected
invocation used `--file=-` and completed the archive comparison.

The provider responses were QA-authored mailbox inference. This proves real
IAM/broker/tool/approval behavior, not unattended model intent recognition or a
Telegram administrator conversation. Native IAM cache TTL stayed zero; enabled
cache and lost-COMMIT behavior are covered by the focused behavioral tests.
Browser CA import and interactive bidirectional zitadeltg acceptance remain
separate user-assisted work. No production deployment or readiness is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
