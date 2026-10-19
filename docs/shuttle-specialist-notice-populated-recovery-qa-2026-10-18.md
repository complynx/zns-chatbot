# Populated specialist notice recovery component QA — 2026-10-18

The real mvp-56 candidate passes archive, restoration and guarded database
promotion checks on an isolated PostgreSQL copy. Both48 recovery data and
populated49 history match their raw fingerprints. Main remains schema48 / mvp-55;
its ten Compose containers did not change. This is component acceptance, not a
complete live-controller rollback or main installation.

## Data preservation

The fixture starts from the retained successful [schema49 rehearsal](shuttle-specialist-notice-schema49-rehearsal-qa-2026-10-18.md)'s
source48 dump, with its recorded SHA checked. It is not a new main backup. After
restoration into the private PostgreSQL, the actual installation controller
creates a fresh48 archive of this copy and independently restores it into an
owned recovery database. All109 baseline fingerprints and the exact48 migration
history match. The recovery database is then fenced.

The exact candidate image runs migrate-only against the copy. The reviewed
transition verifier passes. The fixture then adds one global preference, one
immutable operation receipt, two booking/cancellation notification rows and one
resolution targeting a specialist notice. Existing user/event/booking identities
supply foreign keys only. No Telegram send, business authorization decision,
application worker, real IAM exchange or Loom run is exercised.

Changing only the resolution's new specialist target changes exactly that
resolution table's raw digest. The original target is restored and all112 raw49
fingerprints match again. Thus archive checks include the new target; they do not
use the empty-transition verifier's normalized row digest.

The controller archives this populated49 database and restores it into a third
owned database. All112 raw fingerprints match. The48 and49 archives are
2,820,493 and 2,831,594 bytes. The same guarded promotion component then fences
and quarantines49 and promotes the independently verified48 copy, using exact
cluster/OIDs/owner markers. Both databases remain fenced. Each is temporarily
opened only for its final raw comparison and then fenced again: promoted48
matches the109 baseline, and quarantined49 matches all112 populated fingerprints.
No migration Down or stale47 archive is used.

## Review and cleanup

Five fresh independent review passes found five substantive runner defects:
missing timeout observation, zero-job cleanup initialization, partial-cleanup
recovery, partial-preparation abort, and retained-archive validation. All were
fixed before execution. The final review found no actionable findings. Eleven
synthetic boundary tests pass, covering live/foreign/terminal jobs, connected
DB work, early failure, partial cleanup with signal timeout, partial preparation,
repeated prepare, and missing/corrupt archives. The initial five-case test run
had two assertions against an unmodified pre-JSON fixture rather than saved
state; its exact test source and failure log are retained. It remains failed.
Reviewers executed nothing.

PostgreSQL is bounded to512 MiB/one CPU, with no host port and a private volume.
The migrator and two restore jobs are each bounded to256 MiB/one CPU. All four
containers have exit0/no OOM and were removed by exact recorded ownership/ID.
The private volume and synthetic credential/config files were removed. Archive
hashes were checked before destructive cleanup and again afterward; archives,
terminal receipts and logs remain privately ignored. No global prune was used.

Ignored evidence: `.cache/shuttle-work/specialist-notice-populated-recovery-v1-qa/`
contains the accepted/cleaned checkpoint, summary, raw fingerprints and archives.
Safety logs are in `specialist-notice-populated-recovery-safety-v1-qa/`. Final
revalidation confirms main schema48 / mvp-55, unchanged ten Compose fingerprints,
all866 candidate release inputs unchanged, no QA containers/volume/credentials,
and an empty Git index.

## Acceptance limits

This gate calls the [reviewed controller](shuttle-specialist-notice-install-controller-qa-2026-10-18.md)'s
archive/restore functions through a private SQL adapter and the guarded promotion
component. It does not run the whole controller, recreate a bot, stop/resume a
poller, test application recovery or prove live database backup/ACL fidelity.
Main's standalone provider and business rows are not independently fingerprinted
by this component gate. Real Telegram and Mini App behavior remain separate.

Next is a finite main installation with a fresh quiescent current48 backup and
actual verified recovery point. Populated49 history must remain archived and
quarantined if live recovery is later needed. Full runtime rollback and restart
acceptance remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
