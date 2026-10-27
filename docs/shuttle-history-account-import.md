# Historical account import

`shuttle-import apply-accounts` creates the missing application accounts after
historical archive indexing and separate IAM account reconciliation. It imports
no passes, balances, tasks or confirmed profile names. Telegram names remain
hints; avatar sources remain owner-checked Telegram references fetched on demand.

## Inputs and authority

Use the target application's existing configuration, a current application-admin
JWT in a private file, and a private mapping plan. The configured bot and
organization define the target scope. The operator UUID must identify the current
application account corresponding to the JWT subject. The command verifies the
`zinusya.admin` role in that organization before opening the target store, and
again before and after checking all target IAM accounts during the transaction.
IAM checks use a zero positive-authorization cache TTL.
Before importing, the command checks the database deployment binding against
the exact configured bot, organization and issuer, using the normal startup guard.

Example plan shape (placeholder UUIDs/subjects only):

```json
{
  "operator_user_id": "OPERATOR-APPLICATION-UUID",
  "request": {
    "archive_id": "APPLIED-ARCHIVE-UUID",
    "sha256": "LOWERCASE-ARCHIVE-SHA256",
    "mappings": [
      {"record_id": "APPLIED-USERS-RECORD-UUID", "subject": "RECONCILED-IAM-SUBJECT"}
    ]
  }
}
```

A mapping selects an exact applied `users` record. The store revalidates the
archive and source locator and derives the numeric Telegram owner itself. Do not
copy old IAM fields into the mapping as authority. For each target, the command
reads the exact active IAM account and verifies its organization and verified
synthetic email for the derived bot/numeric Telegram ID. Read names are discarded.
This is an explicit operator reconciliation path; it does not change the
external-provider proof contract or infer an OIDC subject from a numeric ID.

The plan is bounded to 1 MiB and at most 1000 mappings per atomic batch. Unknown
fields and additional JSON documents are rejected. Credential input is bounded
to 16 KiB. Private inputs, account identifiers and underlying errors are not
printed to the console.

## Execution

Apply migrations through the normal release procedure first. The command does
not run migrations, start consumers or contact Telegram/Loom. Use the ordinary
migration/cutover ownership and backup rules; production execution requires its
separate authorization.

```sh
shuttle-import apply-accounts -config /private/app.yaml -plan /private/accounts.json -authorization-file /private/operator.jwt
```

The release Dockerfile includes `/usr/local/bin/shuttle-import`; override the
ordinary bot entrypoint for this operator command and mount only the required
private files. A build from the modified sources is required before using the
new command; an older accepted bot image does not contain it.

Successful stdout reports only `accounts`, the number of returned receipts. It
includes replayed accounts and is not a count of newly created users. Per-source
receipts remain in PostgreSQL. Existing profile names, language/preferences,
Telegram metadata and exact bindings are preserved. A conflicting source/target
mapping or revoked account rejects the batch. IAM authorization failure rolls
back the uncommitted account and receipt writes.

After an unknown outcome, inspect private receipts and retry the identical plan;
source record IDs are durable idempotency keys. Do not choose a new subject or
operation to bypass a conflict. Replay also checks current account/IAM access.
The additive receipt migration refuses automatic Down after receipts exist;
imported application users are not automatically deleted on rollback.
