# Application schema baseline

The application baseline is schema version 73 in [00073_schema.sql](../migrations/00073_schema.sql). The baseline consolidates the earlier migration chain without assigning a new version to an already complete version-73 database.

A fresh empty application schema is created directly at version 73. A database already migrated through version 73 keeps its existing objects and data; replacing migration source files does not cause those applied migrations to execute again.

A partially migrated legacy database must finish the original chain through version 73 using the preceding release. The new baseline refuses a nonempty applied legacy migration history rather than guessing which objects are missing. Do not remove migration history or the deployment identity binding to bypass this guard.

Down is supported only for a fresh database created by the single version-73 baseline, with no accepted data and unchanged initial settings. The guard refuses a legacy applied 1–73 history even when its domain tables are empty; otherwise removing only version 73 would leave misleading earlier migration history. It also locks application tables and refuses retained domain/operation data. River owns its separate migration history. No automatic Down path deletes live data.

Domain documents may mention the historical migration that introduced a field or table. Those numbers describe feature provenance; current fresh-schema construction is the consolidated version-73 baseline. Compatibility checks must compare the actual schema and preserved data, not infer equivalence from one version number.

See the [deployment identity invariant](shuttle-deployment-scope.md), [historical account import](shuttle-history-account-import.md) and [QA procedure](shuttle-qa.md).
