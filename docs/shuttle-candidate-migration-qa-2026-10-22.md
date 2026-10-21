# Local candidate migration and rollback check

On 2026-10-21 UTC, copies of the owned `shuttle-live-test` application and Loom databases passed migration and backup-restore checks in an isolated PostgreSQL container. The primary stand was not stopped or changed.

## Scope and versions

The original application was source `3be6e40b`, schema69 / manifest `mvp-89`, image `sha256:e23375962bda17b0ffc7d5b34e220e95bf03cccb0169dfc10739955cc304ef2c`. The candidate was application source `4f03e5ff`, schema70 / `mvp-91`, image `sha256:a9c2d76e5f3a01889ef041e4bfba8131433a84241829e6cbd1656815e00869fd`.

The original Loom API image was `sha256:a75493893b4ded4a679679ac04ad32b5e903a3b946b53c208554e33ac90095e7`. Its replacement was published revision `34f4646832748efd455828f6804c6274027db659`, image `sha256:003529b117602d52905e1d5f65d353fbe45e969dfc065eae638627b379f813ff`. Loom application schema remained4.

The application source has a successful [full CI](https://github.com/complynx/zns-chatbot/actions/runs/39678246176). The [separate actual-IAM operation handoff](shuttle-loom-adoption-acceptance-2026-10-21.md) proves the new runtime contract; this check addresses preservation of existing stand data.

## Executed check

1. Check the current primary preservation record and confirm no active business task or Loom run. Dump only its two local databases and fully decode each custom archive before use. These are separate per-database snapshots; they are not a cross-database atomic production backup.
2. Restore the archives into a disposable, memory-limited PostgreSQL instance on an internal-only network, with no published ports or connection to the primary network. Keep credentials and archives in an ignored directory with owner-only Windows ACLs.
3. Record counts and deterministic row digests for all127 application and37 Loom public tables, plus sequences. Invoke only the new images' migration commands, without starting their servers, workers or Telegram polling.
4. Verify all original application data except migration metadata. The comparison removes the new `original_expired_at` JSON field from the row projection; separately verify that all new marker values are NULL. Verify all other application sequences unchanged. The application advances69 ->70, while the complete Loom table/sequence snapshot stays unchanged.
5. Restore the original archives into the clone with clean/if-exists restore. Run the original images' migration commands. Compare both databases against the entire original table/sequence snapshots, including migration metadata: they match exactly.
6. Recheck primary domain/runtime preservation. Remove the owned clone and migration containers, its anonymous volume and its internal network after retaining the logs, archives and comparisons.

The original snapshots cover1779 application rows and5212 Loom rows, including scheduler and migration tables. Both images completed their migration commands successfully. This does not claim that the old servers were started or that application interaction was tested during rollback.

## Installation conditions

The original stand retains one unknown pair-assignment operation and one prepared order-payment review, both belonging to cancelled runs. They are pre-existing evidence, not work to resend. A real installation must preserve these operation documents and states and must not interpret an unknown response as permission to repeat an effect.

The concrete installation plan keeps the original Telegram poller fenced until old writers are stopped, fresh fenced backups are verified, all Loom API/run workers are upgraded, the application migration is applied, and preservation/readiness checks pass. Only then may the original poller resume. The new adoption capability must not be enabled with mixed old/new Loom workers.

Rollback by restoring pre-installation copies is limited to a failed installation before ingress resumes and before any new work is accepted. After resumption, restore is not automatic: reconcile/drain new operations and plan recovery explicitly. Schema70's Down migration refuses after browser-source expiry; the guard must not be bypassed.

This is a clone-only preservation gate, not acceptance of primary installation, installed-runtime recovery, real Telegram WebView/ZitadelTG, autonomous model reasoning, production export or production cutover. No Loom developer checkout or stand was changed.

Private evidence remains under `.cache/shuttle-work/primary-candidate-migration-20261022-v1/`; the independently reviewed controller is `.cache/shuttle-work/primary_candidate_migration_20261022_v1.py`. The concrete next-stage plan is `.cache/shuttle-work/primary-candidate-install-20261022-v1/plan.json`. Fixture credentials and raw data remain ignored.
