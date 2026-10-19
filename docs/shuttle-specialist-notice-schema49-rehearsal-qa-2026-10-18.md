# Specialist notification schema49 rehearsal QA — 2026-10-18

The real candidate image `sha256:1f9d123de594fd40dc27e18f56576dd0ac91046077074de130e85464dce9561a`
passes a schema48-to49 rehearsal on a separate PostgreSQL copy. Dump, archive
listing, restore, migration and a deliberately separate repeat migration each
exit0. The repeat is a no-op. Four deliberate corruptions are rejected and
restored. Main remains schema48 / mvp-55; it was neither migrated nor restarted.

The source is a read-only dump of our owned `shuttle-live-test` database. The
2,820,769-byte archive is retained privately under the ignored QA directory.
It was restored into a fresh owner-labelled PostgreSQL container with no host
port and a private volume. The exact candidate image runs only `-migrate`, using
synthetic startup credentials and that copy's connection string. No application
workers, real Telegram sends, IAM exchanges or Loom runs are part of this gate.
The image was [built and reviewed separately](shuttle-specialist-notice-candidate-image-qa-2026-10-18.md).

## Accepted preservation checks

The restored48 baseline has 100 public tables and 109 fingerprints. The49 result
has 112 fingerprints, adding exactly three empty tables. All old public table
row values and sequence state remain unchanged except the exact Goose ledger
advance. The old resolution row is retained. Its digest omits only the newly
added null specialist target column; its old fields are compared in full, and
any non-null new target is separately forbidden before activation.

Old column, index and constraint definitions are compared with only the intended
new resolution column/FK and replacement one-target check excluded. Those
excluded changes are then checked independently: ordinary nullable UUID with no
default, exact FK target/key and actions/validation/deferral, and the exact
five-target CHECK. PostgreSQL deparses an expected temporary CHECK; FK metadata
is checked directly through catalogs. This avoids both blanket table exclusions
and guessed CHECK formatting.

The successful baseline verification is followed by four adversarial changes:
existing resolution request-hash bytes, a weakened one-target CHECK, a non-null
UUID default on the new column, and removal of its FK. Each is rejected, then
restored. After each restoration the complete transition fingerprint matches
again. Final verification passes, as do the ten main Compose container
fingerprints and the unchanged source48 migration history.

These transition fingerprints normalize the new target only while it is empty.
They must not replace raw full-row fingerprints when archiving a populated49
candidate or validating its retained history during rollback.

## Review, failures and resource cleanup

Fresh correctness/runner reviews led to fixes before execution: use an existing
field for the tamper, bounded waits, observation instead of unknown-effect
replay, failed-preparation abort, and invocation-bound failure handling so a
repeated prepare cannot overwrite an existing checkpoint. Eight synthetic
behavioral safety tests pass. Their initial two mock-call accessor errors are
retained in the safety evidence directory.

The first runtime attempt migrated successfully but its verifier tried to give
a temporary table an FK to a permanent table, which PostgreSQL rejects. The
second migrated and repeated successfully; three tamper tests passed, but the
missing-FK case returned SQL NULL and caused a JSON parsing error. Its tamper
was restored before cleanup, and main was checked unchanged. Both attempts stay
failed. Their logs, exact used scripts, archives and summaries are retained.
The corrected verifier uses catalog FK checks and coalesces absent rows to an
empty JSON list. Fresh final review found no actionable findings before the
successful third attempt. Reviewers ran no commands or tests.

PostgreSQL is limited to 512 MiB/one CPU; each migrate-only container to
256 MiB/one CPU, with read-only roots and dropped capabilities. The successful
attempt's two migrators and PostgreSQL all have terminal exit0 and no OOM.
All three attempts' exact clone containers and private volumes were removed.
Archives and logs remain. No global prune was used; the index remains empty.

Ignored evidence: `.cache/shuttle-work/specialist-notice-upgrade-v3-qa/` contains
the accepted/cleaned checkpoint, restored baseline,49 fingerprints, commands and
four rejected tamper kinds. The v1/v2 directories preserve failures. Safety logs
are under `.cache/shuttle-work/specialist-notice-upgrade-safety-v1-qa/`.

## Acceptance limits and next step

The data baseline is taken from the restored copy, not an independently hashed
quiescent live snapshot. Dump uses no-owner/no-privileges; this is not a live
backup-fidelity, database ACL/configuration restore, promotion, controller
rollback or production gate. Main-container fingerprints do not establish a
business-row or standalone-provider fingerprint. Startup/real Telegram/native
Mini App acceptance of the complete feature is still pending.

Next is the finite owned installation/recovery controller with these preservation
checks, critical failure tests and review. Actual main migration requires a fresh
quiescent48 archive and verified restore. Preserve candidate49 history on rollback
and never bypass populated-table Down guards or use the stale47 archive.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
