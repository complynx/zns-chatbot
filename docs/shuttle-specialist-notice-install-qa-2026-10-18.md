# Specialist notice owned installation QA — 2026-10-18

The owned `shuttle-live-test` now runs schema49 / mvp-56 from candidate image
`sha256:1f9d123de594fd40dc27e18f56576dd0ac91046077074de130e85464dce9561a`.
Forward installation passes with a fresh, actually restored48 recovery point.
Only the bot container was recreated. The same poller resumed and completed a
fresh cycle. Full live-controller rollback and feature-level Telegram/Mini App
acceptance remain open.

## Backup, transition and activation

The poller stopped first; task/outbox queues drained before bot shutdown. The
bot exited0 in0.688 seconds measured around Docker stop. The poller also exited0.
No forced kill or OOM was observed. This proves a quiet shutdown, not the full
pending-task/uncertain-send restart requirement.

The controller captured109 raw48 table/sequence/schema fingerprints and created
a fresh quiescent current48 archive: 2,820,686 bytes, SHA256
`8c32aa07bc1200d888056d0c51dbe354763fb10f70cff81a34a055c60e73c1a7`.
An independent database restore matched every baseline fingerprint and the exact
Goose history before migration. That recovery database remains fenced under
OID61051 and its exact owner marker; primary OID16385 remains unchanged. The
verified private archive is retained. The earlier47 archive was not used.

One owner-labelled bounded migrate-only container exited0/no OOM. Verification
preserved all prior row data and sequence values except the exact Goose advance,
allowed only the intended old resolution-table changes, and checked the new
nullable UUID FK and exact five-target CHECK. The three new tables were empty.
The49 transition has112 fingerprints. Verification uses normalized resolution
rows only with the new target independently required to be null; populated49
recovery must use the [raw archive component checks](shuttle-specialist-notice-populated-recovery-qa-2026-10-18.md).

Only an image override was allowed in the resolved Compose model. The new bot
passed readiness and published `service_id=zinusya`, revision mvp-56. The live
staff manifest has seven methods, including preferences/set_preferences, with
the write's idempotent status path. After activation, the fingerprint comparison
changed only River leader/queue tables; old business data and settings matched.
Eight Compose companions and the standalone counted provider retain their
recorded container/image/start/restart fingerprints. Bot settings also match.
PostgreSQL, IAM, Loom and the poller were not recreated.

## Preserved failure and reviewed repair

The first post-migration verification failed with `Incorrect target check`.
Migration itself had exited0. Diagnosis proved that the actual CHECK matched the
expected one: the controller's `psql -XAt` included BEGIN/CREATE TABLE/ROLLBACK
status lines, while the accepted private component adapter used `-XqAt`.
The mismatch was in verifier output, not database shape. No bot was started
and no migration was replayed while investigating.

The exact output-only quiet switch fix passes a new boundary assertion that
fails against the archived old SQL function. All23 critical controller tests
pass. Two fresh independent reviews found no actionable findings in the fix and
one-time journal amendment. The amendment checks the exact single-switch source
change, old journal bytes, every unchanged input, stopped services, primary
identity and the same owned terminal migrator; it runs full49 preservation again
before recording the reviewed input hashes. Standard controller advancement then
observes that same completed migrator and verifies49 again. Original source,
source gate, journal, failure and diagnostic evidence remain privately retained.
The first failed check stays failed; its repair is explicit in the audit.

After activation, the first ingress observation was too early for a completed
fresh long-poll cycle. The same container was confirmed live and observed again;
it was not restarted. A later cycle passed. An initial final-receipt probe also
wrongly expected a separate preference_operation manifest method. Source and
live contract expose the write's status_path instead; the probe was corrected,
its failure retained, and the final receipt passed without product changes.

## Cleanup and acceptance limits

The restore and migrator jobs have terminal exit0/no OOM and were removed by
exact recorded IDs/ownership. Temporary pg/migrate env files were removed. The
fresh fenced48 database and archive remain intentionally as recovery artifacts;
no PostgreSQL volume was removed. The new bot and original poller are running,
readiness passes, the archive hash matches, and the Git index is empty.

Ignored evidence: `.cache/shuttle-live-test/specialist-notice-install-v1/`
contains the accepted49 checkpoint, final summary, preactivation receipt, fresh
archive, retained verifier failure/source/journal and early ingress/final-probe
observations. `.cache/shuttle-work/specialist-notice-quiet-output-repair-v1-qa/`
contains the reproduced old-output failure,23-case pass and accepted repair gate.

This accepts forward installation and a quiet shutdown. It does not prove the
whole live rollback controller, pending task/outbox restart recovery, real
booking/cancellation specialist sends, mute/revoke generation fences at runtime,
agent/native preference continuation or browser Mini App behavior. Those remain
next gates. No production readiness or publication is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
