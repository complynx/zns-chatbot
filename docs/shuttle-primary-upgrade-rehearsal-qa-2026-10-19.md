# Owned primary upgrade rehearsal, 2026-10-19

## Scope

The schema49/mvp56 primary stand remains unchanged. The accepted Linux/amd64
schema59/mvp68 image was tested against a restored copy of its actual database:
`sha256:68d82bd9d01e15a6c04464e36555c4e9252acd4a273f93b2f96798a2a74292f4`.
This is migration and restore acceptance, not a primary installation or production
release. No Telegram message was sent and no Loom developer stand was changed.

## Forward migration

A consistent custom-format pg_dump was retained and its table of contents read.
The source database has103 public tables and one additional history-preview view.
Original-column projections cover all104 relations. Business projections were
read in an explicit repeatable-read, read-only transaction and matched the restore.
River scheduling tables were excluded from the live-source comparison because
normal maintenance can change them; the clone checks include those restored rows.

A separate PostgreSQL container had no network connectivity or published ports,
512MiB memory without swap, and its own labelled volume. The restore and migration
jobs shared only that container's network namespace, with256MiB memory limits.
The candidate used the primary initialization configuration with only its database
URL replaced by the isolated clone URL. Its -migrate path ran both Goose and River
migrations. Normal application workers and Telegram polling were not started.

Versions50 through59 applied successfully. The candidate database has115 public
tables. All original-column projections, including immutable receipts and queue
rows, matched the restored schema49 snapshot. Existing Goose records remained
identical; new records contained exactly the ten expected applied versions.
Existing sequence values and is_called flags remained identical except for the
Goose history sequence, which advanced for those migration records.

Historical order preparations, orders, operations and evidence received
order_kind=service. Existing specialist preferences received notify_next=true,
notify_bookings_version equal to their prior version, and notify_next_version=0.
This verifies existing values and projected rows, not an independent catalogue
comparison of every constraint, default or sequence ownership.

## Restore path

The retained preupgrade dump was restored into another isolated database.
Its old projections, Goose records and sequence state matched the first restore.
The currently installed old image then ran its own -migrate path against that
restored database. All those comparisons were repeated afterward and remained
identical. This establishes a no-op migration startup on the restored database;
it does not establish full old-application behavioral acceptance.

Rollback was tested through restore, not Down migrations. A live rollback must
preserve any history accepted after an upgrade and must not silently replace it
with this earlier snapshot. No overlapping application versions were started.

## Readiness boundary and cleanup

The source had three future, unfinished booked massages. Before installing the
candidate, the controller must account for newly enabled upcoming-session
notifications on these existing bookings, fence ingress, use a fresh backup,
perform graceful shutdown, and verify recovery before reopening ingress.
The current rehearsal does not accept that cutover.

Three private dumps and all five temporary container logs were retained. Their
archive hashes and exact container/image/ownership labels were verified before
cleanup. All four exited jobs, the isolated PostgreSQL container and its volume
were removed. Primary runtime identity and preserved domain state passed the
original observer checks before and after the rehearsal and cleanup.

Ignored evidence is under
.cache/shuttle-work/avatar-primary-upgrade-v1-qa/rehearsal-v1/:
checkpoint.json records migration and restore proof; cleanup-proof.json records
archive verification and exact-owned cleanup. Private dumps and logs must not be
published. The rehearsal script passed a fresh independent review after two
verified proof-ordering findings were corrected before execution.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
