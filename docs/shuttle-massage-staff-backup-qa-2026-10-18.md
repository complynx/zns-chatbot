# Owned massage-staff backup rehearsal — 2026-10-18

Status: PASS for the schema47 backup/restoration rehearsal only. The candidate
schema48 / `mvp-54` application remains uninstalled. The running owned bot stays
on schema47 / `mvp-51`. This is not production acceptance.

The owned `shuttle-live-test` ingress was paused, its business queues drained,
and the same bot container was stopped with a five-second grace limit. The bot
exited normally in 0.641 seconds. All eight Compose companions, including
PostgreSQL, Zitadel and Loom, retained their container/image/start/restart fingerprints.

A private custom-format PostgreSQL dump of the `shuttle` database was created
(2,697,185 bytes), its SHA-256 and archive table of contents checked, and the dump
actually restored with `--exit-on-error --single-transaction` into a unique
owned temporary database. No application migration was run.

The comparison matched 106 fingerprints: all public-table row data, including
migration history; sequence values and `is_called`; sequence configuration;
column definitions; index definitions; and constraint definitions. Source
fingerprints before the dump, after it and after restoration also matched.
This is the stated comparison scope, not a claim to verify every possible
PostgreSQL object or privilege. Sequence WAL bookkeeping (`log_cnt`) is excluded
because it is not part of the restored sequence state.

The verified temporary database was removed after checking its unique ownership
marker. The private archive remains in the ignored stand directory. The same
original bot and poller IDs were restarted, readiness succeeded, and a fresh
Telegram polling cycle completed. Final checks confirmed schema47, unchanged
companion fingerprints, a matching archive hash and no temporary database.
No Telegram messages were sent by this rehearsal.

## Review and preserved failures

Independent correctness and operational-quality review found and corrected two
issues before execution: recovery rejected a prior OOM before attempting restart,
and the sequence comparison included internal `log_cnt`. A later final review
found that readiness failure could prevent restarting the poller; both restart
attempts now happen before readiness validation. Local preparation also exposed
an escaped-newline checkpoint serialization error, repaired before any stop.
The final fresh review reported no actionable findings. The actual rehearsal
completed with driver exit 0. No failure was relabelled as a successful live run.

## Evidence and next boundary

The subsequent [database recovery gate](shuttle-database-recovery-qa-2026-10-18.md)
now proves guarded promotion and actual isolated migration/retained-history
restoration. It does not implement or accept the full installation controller.


The ignored controller is
`.cache/shuttle-work/massage-staff-owned-backup-v1.py`; its checkpoint and private
archive are under `.cache/shuttle-live-test/massage-staff-backup-v1/`.
The checkpoint records the controller hash, review history, source fingerprints,
archive hash, original container IDs, stop duration and restoration result.

The next step is a reviewed schema47-to48 installation/recovery controller and
a fresh quiescent backup at installation time. The rehearsal archive does not
contain activity accepted after ingress resumed. Rollback must preserve any
new candidate binding/operation history; it must not bypass migration48's
populated-table guard. Real IAM/Loom/Telegram native, agent and mixed specialist
and administrator acceptance follows installation and remains open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
