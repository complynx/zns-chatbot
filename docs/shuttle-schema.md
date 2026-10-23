# Application schema baseline

The application baseline is version 74 in [00074_schema.sql](../migrations/00074_schema.sql), the single tracked SQL migration. A fresh empty application schema receives the preserved version-73 DDL and then the version-74 extension. A completed version-73 database skips fresh-schema construction and receives only that extension. Existing migration history, objects and accepted data remain in place.

Version 74 adds `first_name_latin` and `last_name_latin` as non-null text with empty defaults. Profile operations validate and save the Latin pair. A trigger clears both values when either original name component changes, including updates by an older application. The current writer saves the original components first and the Latin pair second in the same transaction; a Latin-only update does not clear the pair.

A partially migrated legacy database must finish the original chain through version 73 using the preceding release. Admission uses the latest effective applied history and rejects versions 1–72 before schema changes. Do not remove migration history or the deployment identity binding to bypass this guard.

Down is supported only for an unused fresh version-74 baseline with unchanged initial settings. Any version-73 or earlier history, including an unapplied record, blocks Down before DDL. The guard locks application tables and refuses retained domain or operation data, including profiles with Latin names. River owns its separate migration history. No automatic Down path deletes live data.

The fresh DDL is conditional PL/pgSQL, so sqlc cannot read it directly. Before `sqlc compile` or `sqlc generate`, run `python3 tools/prepare-shuttle-sqlc-schema.py`. This derives the ignored `.cache/shuttle-sqlc/schema.sql` from explicit fresh/common sections of the same migration; it is not a second maintained schema. The extractor rejects missing or repeated sections, and CI checks it before generation.

Historical migration numbers describe feature provenance. Compare actual schema and preserved data when checking compatibility. See the [deployment identity invariant](shuttle-deployment-scope.md), [historical account import](shuttle-history-account-import.md) and [QA procedure](shuttle-qa.md).
