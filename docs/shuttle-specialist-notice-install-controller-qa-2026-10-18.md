# Specialist notice installation controller QA — 2026-10-18

The initial finite owned schema48-to49 controller source gate passed22 critical
boundary tests and two fresh independent reviews before main was changed. The
subsequent [owned installation](shuttle-specialist-notice-install-qa-2026-10-18.md)
now runs schema49 / mvp-56. It found a psql command-status output mismatch in the
verifier adapter. The exact quiet-output fix passes23 tests and two additional
fresh reviews. The original failed verification and22-case evidence are retained;
full live rollback remains unaccepted.

The controller pins the previously built mvp-56 candidate and accepts only the
owned stand's exact source image, cluster, database OID, settings and migration
history. A prior completed installation marker is adopted only when its accepted
journal matches the same cluster/OID and exact marker. Unknown, incomplete or
foreign ownership is rejected. Recorded inputs are frozen and checked again.

Forward installation requires a fresh quiescent48 archive and an actual verified
restore. Migration49 uses the reviewed empty-transition verifier. Recovery uses
raw full-row candidate49 fingerprints, a verified49 archive restore and a fenced
quarantine before promotion of the verified48 recovery database. It never uses
migration Down or the stale47 archive. Old-image activation requires exact48
history and the recorded database identity. A single-writer lock serializes the
journal. Unknown Docker creation/start acknowledgments require observation,
without repeating the operation. Cleanup retains terminal receipts and archives.

The 22 tests use synthetic Docker, SQL and HTTP boundaries; only the separate
process lock test starts a real child. Cases cover image/schema/OID activation,
exactly-once recreation observation, job ownership and terminal receipts, live
migration rollback rejection, restored identity, OOM recovery, configuration
drift, source-marker adoption and mismatched transition history. Both reviewers
found no actionable findings and executed nothing. The index remains empty.

Ignored evidence: `.cache/shuttle-work/specialist-notice-controller-v1-qa/`
retains the initial22-case log/hashed receipt. Its current source gate records
23 cases and the separately reviewed quiet-output amendment. The repair evidence
is in `.cache/shuttle-work/specialist-notice-quiet-output-repair-v1-qa/`.
The [real-image migration rehearsal](shuttle-specialist-notice-schema49-rehearsal-qa-2026-10-18.md)
is separate evidence. The [populated49 archive/restore/promotion component gate](shuttle-specialist-notice-populated-recovery-qa-2026-10-18.md)
passes with raw target-sensitive hashes and verified48/49 archives. Forward
main installation is now accepted separately; full live-controller rollback,
pending-work process restart and feature-level Telegram/Mini App remain pending.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
