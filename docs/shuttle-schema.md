# Application schema baseline

The application baseline is version 76 in [00076_schema.sql](../migrations/00076_schema.sql), the single tracked SQL migration. A fresh empty application schema receives the preserved version-73 DDL and the version-74/75/76 extensions. Completed versions 73, 74 and 75 receive only their missing extensions. Existing history, objects and accepted data remain in place.

Version 74 adds `first_name_latin` and `last_name_latin` as non-null text with empty defaults. Profile operations validate and save the Latin pair. A trigger clears both values when either original name component changes, including updates by an older application. The current writer saves the original components first and the Latin pair second in the same transaction; a Latin-only update does not clear the pair.

A partially migrated legacy database must finish the original chain through version 73 using the preceding release. Admission uses the latest effective applied history and rejects versions 1–72 before schema changes. Do not remove migration history or the deployment identity binding to bypass this guard.

Version 75 adds the bounded sticker/custom emoji description catalogs, their per-catalog adaptive decay policy and an index for recovering usage from retained inputs. It does not change or backfill existing profile names. See [visual symbols](shuttle-symbols.md).

Version 76 adds chat reaction cursors and independent Telegram reaction projections. Desired state, deferred target mapping and immutable uncertain-attempt receipts survive restarts. It does not backfill reactions for older runs that did not negotiate the reaction policy.

Down is supported only for an unused fresh version-76 baseline with unchanged initial settings. Any version-75 or earlier history, including an unapplied record, blocks Down before DDL. The guard locks application tables and refuses retained domain or operation data. River owns its separate migration history. No automatic Down path deletes live data.

The fresh DDL is conditional PL/pgSQL, so sqlc cannot read it directly. Before `sqlc compile` or `sqlc generate`, run `python3 tools/prepare-shuttle-sqlc-schema.py`. This derives the ignored `.cache/shuttle-sqlc/schema.sql` from explicit fresh/common sections of the same migration; it is not a second maintained schema. The extractor rejects missing or repeated sections, and CI checks it before generation.

Historical migration numbers describe feature provenance. Compare actual schema and preserved data when checking compatibility. See the [deployment identity invariant](shuttle-deployment-scope.md), [historical account import](shuttle-history-account-import.md) and [QA procedure](shuttle-qa.md).
