# Application schema baseline

The consolidated application baseline is version 76 in [00076_schema.sql](../migrations/00076_schema.sql).
A fresh empty application schema receives the preserved version-73 DDL and the version-74/75/76 extensions.
Completed versions 73, 74 and 75 receive only their missing extensions. Existing history, objects and accepted
data remain in place. Later changes use ordered additive migrations; the source includes version 79.
This is a source schema description, not a claim about the installed production version.

ZiNuSya is live. Keep published migrations and applied history immutable. Add forward
migrations for changes; never recreate the baseline, clear history or replace retained
data to admit a new release. A fresh-schema test does not establish upgrade safety.

Before connecting, run `zinusya -config config/zinusya.yaml -check-config` to validate
startup YAML and environment overrides. This mode makes no network or database
calls and starts no workers. It does not verify remote credentials or schema state.
Ordinary startup must apply pending application and River migrations with the
migrator credential (`migration_database_url` / `ZNS_CHATBOT_MIGRATION_DATABASE_URL`),
then check the resulting schema and runtime permissions before runtime work. Migrator and runtime connections must identify the same database and
schema. Keep DDL rights off the runtime role and close the migration connection after
preparation. Explicit `-migrate` remains an operator mode that applies migrations and
exits; it cannot be combined with `-check-config`.

Treat startup, populated-schema upgrade, repeated startup and compatible old/new
readers or writers as separate acceptance cases. Preserve settings, object identities,
original receipts and unresolved outcomes. See the [required checks](shuttle-qa.md#live-upgrades-and-restarts).

Version 74 adds `first_name_latin` and `last_name_latin` as non-null text with empty defaults. Profile operations validate and save the Latin pair. A trigger clears both values when either original name component changes, including updates by an older application. The current writer saves the original components first and the Latin pair second in the same transaction; a Latin-only update does not clear the pair.

A partially migrated legacy database must finish the original chain through version 73 using the preceding release. Admission uses the latest effective applied history and rejects versions 1–72 before schema changes. Do not remove migration history or the deployment identity binding to bypass this guard.

Version 75 adds the bounded sticker/custom emoji description catalogs, their per-catalog adaptive decay policy and an index for recovering usage from retained inputs. It does not change or backfill existing profile names. See [visual symbols](shuttle-symbols.md).

Version 76 adds chat reaction cursors and independent Telegram reaction projections. Desired state, deferred target mapping and immutable uncertain-attempt receipts survive restarts. It does not backfill reactions for older runs that did not negotiate the reaction policy.

Version 77 adds durable local Telegram ban receipts with required actor and reason.
It does not backfill bans or modify existing account access. Its Down migration refuses
nonempty audit history. Apply prepared restrictions separately before accepting users;
see [local bans](shuttle-user-bans.md).

Version 78 separates optional public ban text from the required internal reason.
Existing accounts and receipts receive empty public text. Published text is kept
in the ban audit; its Down migration refuses retained public text.

Version 79 adds bot-scoped Telegram transport availability and a revision fence.
Confirmed blocked/deactivated recipients pause without losing account or business
data. Fresh committed private intake resumes them; duplicate input and stale send
failures do not reverse that decision. Down refuses active pauses. An older binary
can retain the additive table but does not enforce the new pause behavior.

The version-76 baseline Down is supported only for an unused fresh installation with unchanged initial settings. Any version-75 or earlier history, including an unapplied record, blocks Down before DDL. The guard locks application tables and refuses retained domain or operation data. River owns its separate migration history. No automatic Down path deletes live data. Application rollback and schema rollback are separate decisions; a runnable older binary does not prove equivalent behavior.

The fresh DDL is conditional PL/pgSQL, so sqlc cannot read it directly. Before `sqlc compile` or `sqlc generate`, run `python3 tools/prepare-shuttle-sqlc-schema.py`. This derives the ignored `.cache/shuttle-sqlc/schema.sql` from explicit fresh/common baseline sections and ordered additive Up sections. It is not a second maintained schema. The extractor rejects missing or repeated sections and excludes Down DDL; CI checks it before generation.

Historical migration numbers describe feature provenance. Compare actual schema and preserved data when checking compatibility. See the [deployment identity invariant](shuttle-deployment-scope.md), [historical account import](shuttle-history-account-import.md) and [QA procedure](shuttle-qa.md).
