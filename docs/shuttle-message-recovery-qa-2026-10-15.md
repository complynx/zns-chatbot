# Message recovery and corrected-image QA — 2026-10-15

The corrected image is
`sha256:8323e6f63d4b2a8967527378ea5dbff9eb7f7fa84076071ef528f64327040c7a`,
manifest `mvp-33`, database schema 32. It contains the event-context pricing schema
fix and uncertain accepted-message recovery described in the
[recovery contract](shuttle-message-recovery.md).

## Behavioral proof

The prepared Input restart scenario failed against the exact old task engine:
no second attempt reconciled the saved intent. With the corrected engine, focused
PostgreSQL restart/order scenarios passed, including receipt loss and unrelated
conflicts. All 261 enumerated integration tests passed in 18 bounded Linux/race
batches on their first attempt in this run. The remaining explicit command,
internal and migration packages passed under race. Test receipts were reconciled
against the inventory to detect omissions or duplicate coverage.

The integration peer is synthetic Loom. It models retained receipts before
terminal rejection and records attempts separately from effects. These tests
prove bot behavior under that contract. A separate real-service probe below
verifies session-to-fresh-delegation acceptance.

## Artifact and native runtime proof

The full Dockerfile build froze 662 inputs. The Linux/amd64 image is 57,345,139
bytes, runs as 10001:10001 and uses SIGTERM. Inspection verified all 393 files in the retained dependency notice/archive payload,
CA material and ELF executables. The archive includes upstream source and
configuration files; application source, application configuration, cache and
shell are absent. Avatar conversion and invalid-input behavior
passed; the generated image was also visually inspected.

A separate fresh PostgreSQL database ran normal migrations. With real local IAM
and synthetic Loom/Telegram peers, the native app became ready and exposed
`mvp-33`. SIGTERM while a run read was blocked, followed by restart, recovered the
same task/run/session without duplicate creation or delivery. Observed exit upper
bounds were 431.455 and 376.510 ms; drain logs reported 17.107 and 4.360 ms.
Both app exits were zero, without OOM or rescue kill. These observations are not
a whole-app worst-case five-second shutdown proof.

Temporary builders, runtime probes and the disposable test PostgreSQL were
removed, including their anonymous volumes. Shared build selection and Loom
developer stands were unchanged. The previous main-stack image remains available
for rollback. The main owned stack was then restored on this exact image using a separate
guarded rollout. Before enabling ingress, it verified unchanged environment,
configuration and business-table snapshots, retained companion container IDs,
real-IAM owner API access and bot/Loom readiness. The existing poller was checked
for bot 6087685431, owner account 8968148043 and the owned webhook. A fresh
successful Telegram polling cycle was observed after startup. No Telegram message
was sent as part of this restoration; this is ingress startup proof, not a new
end-to-end conversation test.

## Real IAM/Loom receipt contract

Two new synthetic runs on the owned real Loom verified the contract separately
from bot restart tests. After terminal cancellation, an unaccepted Input and a
new-key Input both returned structured HTTP 409 `run_terminal`; there were zero
persisted input rows. On the second run, Input was first accepted using an auth
session. After cancellation, replaying its exact body/key with fresh subject
delegation succeeded. A new key returned `run_terminal`, and exactly one input
row remained after all requests. Both recorded runs are terminal. No Telegram
message, business tool or model-mailbox response was sent by this probe.

The first probe did not pass outright: cancelling the second run returned HTTP
503. Its checkpoint and original failure were preserved. A separate recovery
continued on that same recorded run using fresh delegation, completed cancellation
and proved receipt replay plus the one-row invariant. The original 503 response
body was not retained, so its root cause is unclassified. This does not prove
behavior with an actually expired auth session or unattended model execution.
Evidence is in `.cache/shuttle-live-test/steering-real-loom-contract/checkpoint.json`
and `recovery.json`; the original checkpoint deliberately remains `passed:false`.

## Review and retained evidence

Independent source reviews covered correctness, Go/code quality, architecture and
recovery semantics. A native-QA review found a test assertion that could fail
while holding a peer mutex; it was fixed and freshly reviewed. Scoped lint ended
with zero issues. Initial lint failures, the old-engine failing regression, a
later CRLF formatting failure and a build-helper entrypoint typo before effects
remain recorded rather than being overwritten.

Ignored evidence lives under `.cache/shuttle-work/cross-module-review-qa`,
`.cache/shuttle-work/container-results`, `.cache/shuttle-work/steering-image-qa`
and `.cache/shuttle-live-test/steering-image-runtime-qa`. It includes frozen source
hashes, per-container exit/OOM receipts, complete coverage reconciliation, build
and inspection results, task snapshots and native drain logs. It is local evidence,
not a published artifact or a production deployment.

The IAM/Loom outages and model backend do not become production-accepted through
these gates. Interactive zitadeltg linking and CA trust remain deferred. No new
commit, push, release or production mutation was made.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
