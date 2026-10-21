# Meal schema69 migration rehearsal, 2026-10-21

Status: clean bounded rehearsal passed on a restored copy of the owned local primary.
The primary remains schema68 / mvp-86. Full release integration tests and primary
installation acceptance remain open.

## What ran

The unchanged release Dockerfile produced candidate image
`sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69`.
The complete image built in a bounded 3 GiB builder after a retained 1.5 GiB
compiler OOM. Both builders have been removed. This report covers migration
behavior, not serving the bot from this image.

A consistent custom-format dump of the running primary's shuttle database was
fully decoded and restored in a new isolated PostgreSQL 17 container. The source
had four order proofs and five review preparations. Restore used --no-owner and
--no-acl; this rehearsal does not prove role/ACL restoration. The original dump
retains that metadata for a separately controlled restore.

The migration container uses public placeholder configuration and only the copy's
database URL. It runs the image's -migrate entry point and exits before composing
Telegram, IAM or Loom clients. It shares no network with those services. Primary
access consists of read-only observation and the database export.

## Proven checks

The copy follows schema68 -> schema69 -> schema68 -> schema69. Forward and repeat
forward operations use the actual candidate migrator. The rollback uses the
embedded migration's Down SQL inside a transaction with Goose's PostgreSQL
version-row deletion. This describes the clean second rehearsal. The first
rehearsal required the separate bookkeeping repair described below.

Row counts and ordered JSON digests cover every public table. Existing rows stay
unchanged except Goose's expected version records. The comparison omits only the
new presentation field from legacy preparation rows, and separately verifies
that every new presentation value remains empty. Application sequence values
stay unchanged; only the expected Goose version sequence advances.

Column checks compare visible order, defaults, nullability and data types.
Constraint checks retain every prior definition except the intended expanded
order-kind check and the new presentation-size check. Reapplication returns the
same logical schema. These are column/constraint checks, not a separate index,
physical-layout or privilege acceptance claim.

A repeat migrator call changes neither rows nor sequence values. Original meal
and service history is not converted into new credits or orders. The primary's
domain and runtime readback remains unchanged throughout.

## Rollback boundary

The copy has no new meal proof or separate consent presentation. Down SQL refuses
rollback once either exists. Those guards have separate behavioral integration
coverage in the [shared evidence stage](shuttle-meal-payment-evidence.md).

Do not infer that a running upgraded stand can always be downgraded. Before
ingress resumes, a fenced backup is the recovery boundary. After new work is
accepted, preserve or export that work before considering a restore; do not
automatically replace the database with an older dump. This rehearsal performs
no primary rollback or cutover.

## Retained failed QA and review

The first review found that physical column positions survive a dropped column.
The comparison now uses visible column order, while still checking all prior
columns. The original review and its correction are retained.

The initial rollback fixture incorrectly appended an is_applied=false row.
Goose 3.28's PostgreSQL Down implementation deletes the version row instead.
The next forward invocation therefore did not reapply schema69. This was a
rehearsal bookkeeping error, not a product migration failure.

The failed checkpoint and a fully decoded copy dump were retained. With the copy
already physically at schema68, the resumed probe removed only its version69
markers and used a distinct forward operation. It did not repeat the successful
Down or export the primary again. Re-up, logical schema/data preservation and a
subsequent no-op then passed. Fresh review of the final controller and resume
code found no actionable findings.

A second isolated copy reused the retained, hash-verified original schema68
dump. It performed forward, no-op, atomic Down plus version-row deletion, and
forward again without a bookkeeping repair. Data, application sequences and
the checked logical schema passed. Its final dump was fully decoded, and its
containers, volume and network were removed. No second primary export was needed.

## Evidence and resource boundary

Private dumps, source hashes, normalized schema, row/sequence digests, migration
logs, original failures and review records are under:
.cache/shuttle-work/meal-financial-flow-v1/schema69-rehearsal-v1/ and
.cache/shuttle-work/meal-financial-flow-v1/schema69-rehearsal-v2/.

Each rehearsal's final copy dump and removal result are recorded in its
checkpoint.json. The second rehearsal records the clean corrected execution.
Retirement removes only this rehearsal's containers, volume and network.
The primary and the independently running full integration gate are preserved.
No production deployment or production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
