# Prepare identities for the users importer

The removable `zns-migrate` command prepares provider identities from a reviewed, bot-scoped users plan. It writes a private identity mapping accepted by the existing users importer. Preparation does not create application users or grant booking eligibility. This is a builder implementation awaiting independent acceptance.

## Operator workflow

First verify and stage the export, then produce and review its users plan with the existing `plan users` command. Resolve source-data blockers before preparation. The identity command accepts only the exact plan regenerated from that verified stage; edited plan bytes are rejected.

Create a private policy file, one explicit decision for every included candidate:

```json
{
  "version": 1,
  "plan_sha256": "<exact users plan SHA-256>",
  "reviewed": true,
  "users": [
    {"legacy_key": "<candidate legacy.key>", "can_book": false}
  ]
}
```

Missing, duplicate, extra or null decisions fail. There is no default allow policy. Review source associations and eligibility deliberately; `reviewed` attests that review. Other-bot records are excluded by the staged manifest, not mapped to this deployment.

Provide these environment variables through the operator's secret mechanism. Do not put credentials in command arguments or shell history:

| Variable | Contract |
| --- | --- |
| `MIGRATE_DATABASE_URL` | Target PostgreSQL DSN with the runtime provisioning schema already migrated. |
| `MIGRATE_IDENTITY_TOKEN` | Current bearer token for a separate organization-scoped identity provisioner; never use the runtime impersonator credential. |
| `MIGRATE_IDENTITY_ISSUER` | Exact HTTPS provider origin, matching the intended imported links. |
| `MIGRATE_IDENTITY_ORGANIZATION` | Intended provider organization. |
| `MIGRATE_IDENTITY_EMAIL_DOMAIN` | Reserved placeholder domain ending in `.invalid`. It is not an account-adoption key. |

Run the preparation command with paths only:

```text
zns-migrate prepare-identities users --stage private-stage --plan users.jsonl --policy reviewed-policy.json --out identity-resolutions.json
```

The complete stage, plan, policy and provider metadata constraints are checked before remote identity calls. Provider first/last names are limited to 200 Unicode characters each and language to ten bytes. Invalid values are rejected rather than truncated. Bot identity comes from the reviewed stage manifest; no command flag can override it.

The command uses the retained official provider SDK. It reserves immutable identifiers, creates or verifies the exact active human provider account, and verifies organization and operation provenance. It never searches by email or treats a numeric Telegram ID as an OIDC subject. Existing application users must already have the exact active durable provider and bot-scoped Telegram binding; unlinked users conflict rather than being adopted.

On success, the private output uses the existing version1 `UserResolutions` contract: plan hash, identity attestation and complete `legacy_key`, `owner`, `issuer`, `subject`, `can_book` mappings. Stdout contains only counts, hashes, reuse status and finite errors. It does not print profiles, identifiers, token, DSN or provider response text. Keep the output private because it contains identity links.

Apply separately after reviewing the generated mapping:

```text
zns-migrate apply users --stage private-stage --plan users.jsonl --resolutions identity-resolutions.json
```

The unchanged importer commits each user, profile, identity links, source metadata and receipt atomically. The explicit policy supplies `can_book`; preparation never enables it automatically. Import replay checks existing state instead of overwriting later policy edits.

## Interruption and recovery

Provider creation and PostgreSQL cannot share an atomic transaction. Preparation therefore keeps durable reservations before remote effects. A lost create response is followed by an exact read of the reserved ID. A rerun uses those same reserved identifiers, verifies the provider again and continues without creating a second identity.

No partial mapping is published. A failure may leave valid provider accounts and reservations for already processed users; these are intentional recovery state. Fix the reported problem and rerun the same reviewed inputs. An expired token can be replaced in the environment before the rerun. Do not manually invent replacement subjects or delete reservations to bypass a conflict.

Output publication uses an exclusive lock and private temporary file. Existing identical output is reused; different output is not overwritten. After an abrupt process termination, an orphan output lock can remain. Verify no preparation process owns it before removing that specific lock. Provider reservations remain the recovery authority; the lock is not the identity journal.

Stopping or removing the migration executable does not require dropping runtime provisioning records. There is no automatic provider-account deletion or claimed rollback across systems. No production invocation is implied by this document.
