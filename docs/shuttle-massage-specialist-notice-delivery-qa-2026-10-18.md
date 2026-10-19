# Massage specialist delivery store QA — 2026-10-18

Fourteen focused integration scenarios pass with the race detector on the final
source snapshot. This gate accepts database reservation and outcome handling,
not external Telegram delivery. The main stand remains on schema 48 / mvp-55;
new migration 49 and this source have not been installed there.

The preceding [storage gate](shuttle-massage-specialist-notice-store-qa-2026-10-18.md)
accepts global account preferences, shared native/agent receipts and transactional
booking/cancellation intents. This stage adds deployment-scoped readiness/read,
current recipient selection, send reservation, completion and startup recovery
operations. A worker must verify IAM before reservation. These database methods
do not perform IAM verification or authorize HTTP callers themselves.

Reservation locks the event and recipient, then rechecks current account, module,
binding, preference and booking state. Exact captured binding and preference
versions prevent stale intents from being revived by revoke/regrant or
mute/re-enable. A specialist does not need a participant pass. Concurrent
reservations for the same version yield one attempt. Completion requires the
same inflight attempt. A known retry keeps the captured generations; the next
reservation repeats all checks. Recovery preserves the original attempt/payload
and marks an inflight delivery unknown, without scheduling an automatic resend.
Its caller must hold the exclusive service instance lock; wiring into service
startup is still open. Tests call the recovery method directly and do not prove
an application restart or external-send outcome.

Cancellation blocks an unreserved creation notice; it preserves a reserved
attempt and creates a separate cancellation notice. Cancellation notification
reservation works with the cancelled booking state. A committed reservation
cannot retract an external message after a later cancellation or permission
change. Contact rendering and the cancelled-contact privacy boundary remain for
the worker stage. No Telegram message or Loom run was created by this gate.

## Verification and retained failures

The final disposable run passes fourteen roots and twenty-six test events with
zero failures or skips. Ten roots retain the preceding storage and adjacent
owner-reminder/binding/native-read checks. Four new roots cover known retry and
receipt fencing, stale authority after caller reads, unknown recovery plus
cancellation, deployment/recipient isolation and concurrent reservations.

Final Linux lint for `internal/db` and `integration` reports zero issues. Fresh
textual reviews of the product and bounded QA runner returned no actionable
findings. The corrected product received another fresh review. Reviewers did
not execute commands or tests. The index remains empty.

The first run failed in a test assertion that compared JSON bytes: PostgreSQL
`jsonb` normalized whitespace. The assertion now compares JSON values. That
failure and logs remain under the v1 evidence directory. An intermediate lint
syntax error in a test helper was also retained; the redundant helper was
removed in favor of the existing payment-scope helper. Final lint and the fresh
v2 race run passed afterward. Production logic did not change for these fixes.

The Go runner has a 2 GiB memory limit and PostgreSQL has a 512 MiB limit. No OOM
occurred. The gate checks frozen source and the ten main Compose services'
container identities, images, start times, running states and restart counts.
It does not establish main business-row fingerprints, IAM/network failure
behavior or full-process restart acceptance. Each attempt owns separate
containers and a private database volume; both attempts are cleaned up after
terminal results. Shared Go caches remain available.

Ignored evidence: `.cache/shuttle-work/massage-specialist-notice-delivery-v2-qa/`
contains the accepted checkpoint, source freeze, exact roots and race log.
The v1 directory retains the failure and final corrected lint output.

Next: wire the Telegram worker, repair scheduler, startup recovery, fixed metrics
and administrative resolution. Add native and agent preference endpoints and
controls, then run multi-user mock Telegram acceptance. Main installation needs
its own reviewed migration/recovery preparation and acceptance.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
