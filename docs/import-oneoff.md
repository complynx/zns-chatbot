# One-time offline import

The importer is a temporary offline tool. Prepare immutable, verified snapshot,
stage, plan and attested resolution files. Apply once in dependency order:
users, events, orders, passes, food, massage, messages. Preserve all original
input hashes and the compiled tool identity in the private execution record.

Stop all managed target writers and verify the exact target, roles and sessions
before applying. An advisory or table lock is not proof that writers are stopped.
Only the assigned operator owns the target during apply and reconciliation.
Identity mappings must be explicit and attested. Unknown identities are never
inferred from names, email or another record.

## Commands and transaction boundary

Use `zns-migrate apply DOMAIN --stage DIRECTORY --plan FILE --resolutions FILE`
with the private `MIGRATE_DATABASE_URL` environment variable. Existing plan,
validation, staging, identity preparation and limit flags retain their meanings.
Each domain command commits one transaction after its input and data checks.
Users, events, orders and messages no longer commit a record or event prefix.
Food, passes and massage already commit their complete domain.

`apply users`, `apply events`, `apply orders` and `apply messages` require fresh
target state for that domain. Repeating them on populated state fails closed.
They do not resume, merge or repair a partial earlier run. Use
`zns-migrate reconcile DOMAIN` with the same flags to verify committed state;
users and events now have explicit reconcile commands too. Reconciliation never
creates missing rows or receipt columns and never backfills dependency snapshots.
Completed-domain verification in the food, passes and massage shared path remains
available, but repeated apply is not an operational recovery contract.

Successful counts describe the committed domain. A failed users/events/orders/
messages transaction reports no applied count, even when earlier rows were
attempted. A failed commit can have an uncertain database outcome; stop and
restore the full target rather than inferring state from the response.

## Whole-target recovery

Before import, bind a complete preimport database snapshot to the exact target,
schema, role/configuration bytes and artifact hash. Alternatively, retain the
exact seed-free schema/bootstrap/role/configuration inputs for full recreation.
Verify that the baseline has no business data and that its artifact bytes are
accessible. A backup filename or successful dump alone does not prove restore.

On any failed multi-domain import, stop. Preserve diagnostics and the immutable
inputs. Stop target writers and verify zero managed sessions. The assigned
lifecycle operator explicitly restores that complete baseline or recreates the
entire allocated synthetic target using the same bootstrap inputs. Verify the
preimport state before reapplying all seven domains with identical stage, plan
and resolution bytes. Compare mappings, permanent references, meaningful domain
state and original proof/history bytes with independently specified projections.
Generated internal identifiers may differ; original identity/ownership, amounts,
timestamps and retained bytes may not.

The importer performs no database reset, drop, partial row repair or restore.
It never deletes a user's database automatically. Production execution requires
its own explicit authorization and target/backup/writer-stop plan. Synthetic
reset success grants no production permission.

## Removal and acceptance

Archive private inputs and temporary receipts before separately authorized
receipt removal. Keep permanent identities, source hashes, legacy references,
proof ownership, full history bytes, explicit unavailable-file states and native
restricted-role enforcement. Mongo/importer dependencies remain outside runtime
builds. Importer-free runtime and EN/RU user flows retain their independent gates.

The active E contract replaces per-record interruption/resume and a universal
crash matrix with one demonstrated whole-target reset/reapply. It does not
accept unexecuted scenarios, weaken runtime durability or replace fresh Code and
Functional QA. Existing E205 tool/runtime evidence belongs to its frozen old
composition; it does not qualify this simplified module.

Written by importer_oneoff_developer (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
