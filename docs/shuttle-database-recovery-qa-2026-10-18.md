# Owned database recovery QA — 2026-10-18

Subsequent [owned forward installation](shuttle-massage-staff-install-qa-2026-10-18.md)
now passes at schema48 / `mvp-54`. This report retains the earlier component
checkpoint below; it does not establish complete live-controller rollback.

Status: PASS for the guarded promotion component and finite migration/restore
rehearsal on disposable PostgreSQL 17. The full resumable installation controller
is not implemented or accepted. The primary owned stand remains schema47 /
`mvp-51`; no candidate application or poller was started by these gates.
This is not production acceptance.

## Guarded promotion

The recovery helper checks the recorded PostgreSQL cluster identity, both database
OIDs, private ownership markers and the allowed name mappings before mutation.
It serializes each step with transaction advisory and catalog locks. It fences
new connections, renames the candidate to quarantine, then renames the restored
database to the application name. The outer controller must stop writes and
independently verify both archives/restores before calling it. It does not
archive, restore, activate an application or authorize the old image.

Eleven behavioral groups passed on actual disposable PostgreSQL:

- Fenced preconditions, normal promotion and repeated calls; both data sets remain
  intact. Repeating completed steps leaves an already opened restored DB open.
- Lost acknowledgment after each committed rename, followed by observation and
  replay without another effect.
- Foreign occupation of either recovery name, changed ownership marker, wrong
  cluster identity, missing OID and unexpected name/OID mapping: all fail closed
  without changing the observed catalog.
- Concurrent duplicate quarantine and promotion requests.
- An existing client blocks rename, new connections are denied after fencing,
  and recovery continues after terminating the exact fixture client.

## Actual image migration and retained history

A separate network-isolated PostgreSQL instance restored the accepted private
schema47 rehearsal archive. All 106 recorded public data/schema/sequence
fingerprints matched. The pinned candidate image
`sha256:72acda129fa105c9830de1bb8a1c6ca28616aecb8f1823c981e0853d94e7ada3`
ran its real application and River migrations using synthetic startup credentials.
No IAM, Loom or Telegram service was contacted by this rehearsal.

The resulting Goose history preserved every original row and added exactly one
applied version48 row; its sequence advanced accordingly. All old table data,
other sequence states/configuration and the old tables' column/index/constraint
definitions matched. Exactly the three expected new tables were added empty.
This comparison covers the stated public-schema objects, not all possible
PostgreSQL objects or privileges.

The gate then seeded one specialist binding, one operation and one operation
alias. The migration's guarded Down SQL rejected rollback with only operation
history populated, and again with all three tables populated. Both rejections
preserved the full recorded fingerprints. The Down source matched the candidate
build's recorded migration hash; this was not a Goose CLI downgrade.

A new custom-format candidate48 archive passed SHA-256/TOC checks and actual
restoration into another database with matching populated fingerprints. The
original47 archive was independently restored again and matched the original
106 fingerprints. OID/marker-guarded promotion switched that restored database
into the application name while preserving the populated48 database under its
quarantine name. Reads after promotion proved both histories intact. Completed
steps were repeated without closing the restored database.

These data sets were preserved through the switch and verified before removing
the disposable PostgreSQL container. Private archives and evidence remain in
ignored local storage. This does not claim that a quarantined database remains
running after disposable QA cleanup.

## Review, failures and cleanup

Fresh source review completed in two passes for each helper/gate. Promotion
review corrected an undersized fixture storage budget. The first actual
promotion run exposed PostgreSQL OIDs serialized as JSON strings; an explicit
numeric SQL projection fixed that mismatch. Its failed, cleaned receipt remains
on disk. The first migration rehearsal proved binding preservation only; the
review-required operation/alias and history-only rejection coverage was added,
and a fresh complete run passed. The initial narrower receipt remains preserved.
Final source reviews reported no actionable findings.

Only unique owner-labelled disposable containers were created; no host ports or
Docker network were created. Limits were 768 MiB for the promotion PG, and 1 GiB
PG plus a 256 MiB migrator for the upgrade rehearsal, with swap disabled. Every
created container was observed terminal and removed by exact ID after ownership
checks; no OOM was observed. The ten primary Compose container fingerprints
remained unchanged, and primary readiness returned HTTP 200 on schema47. The two
reusable mock QA stacks remain stopped. No real Telegram message was sent.

Ignored implementation/evidence paths:

- `.cache/shuttle-work/massage-staff-promotion-v1.py`
- `.cache/shuttle-work/test-staff-promotion-v1.py`
- `.cache/shuttle-work/staff-promotion-v1-qa/`
- `.cache/shuttle-work/rehearse-staff-upgrade-v1.py`
- `.cache/shuttle-work/staff-upgrade-rehearsal-v1-qa/`
- `.cache/shuttle-work/staff-upgrade-rehearsal-binding-only-qa/`

Next: implement and review the resumable owned installation controller, including
exclusive checkpoint ownership, exact migrator observation, verified restoration
and explicit schema47/image/config gates before old-image activation. Neither
binary currently enforces this schema guard itself. Take a fresh quiescent
backup before installing on the primary stand; the rehearsal archive is stale
for that purpose. Application/poller interruption recovery and real-service
native/agent/mixed acceptance remain separate gates.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
