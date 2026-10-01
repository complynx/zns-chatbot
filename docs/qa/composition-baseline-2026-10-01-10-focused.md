# Focused native Go/PostgreSQL reproduction 10

Terminal result: **9 PASS, 1 FAIL, 0 SKIP, 0 unfinished** across the ten named
integration tests. The command exited 1. All seven run09 missing-role cases
passed after the setup lead's separate roles-only bootstrap. Meal-runtime and
large-choice cases passed in this focused run. The deleted payment-card case
reproduced `order_not_found`.

This is bounded reproduction evidence on schema089 source. It is not a full
baseline10, final090 proof, Functional QA acceptance, or a release gate. One
focused meal-runtime pass does not resolve or explain the prior run09 failure.

## Fixed epoch and stand

- Source: `5057ddb0547f5b099459fe3d321c04a7c4490c90`.
- Clean branch/worktree: `codex/native-baseline-10-focused`,
  `.worktrees/native-baseline-10-focused`.
- Native Go 1.27.0 windows/amd64; `GOWORK=off`, `GOMAXPROCS=2`,
  `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`.
- Own build cache in ignored `qa.local/focused-10/gocache`, seeded from the
  runner's terminal run09 cache. Existing native module cache reused.
- Sole test/data writer: `/root/baseline_09_runner`, assigned by root after the
  setup lead's explicit release. Frozen endpoint: `127.0.0.1:58441`.
- Private owner-bundle hash remained
  `08D2B2F1CA73BFAC1374A4AA90087C2B3A40DCA5CE87FA8B5CF8C8958C3ACE44`.
  The runner checked the actual private DSN endpoint without printing it.

Before tests, a native pgx connection verified PostgreSQL 17.11/170011 and read
`pg_authid` only. `zns_api`, `zns_bot`, and `zns_meter` have LOGIN and a stored
password; superuser, CREATEDB, CREATEROLE, replication and bypassRLS are all false.
`zns_app` is absent. The role readback was PASS. See
[the allocation and roles-only bootstrap receipt](native-baseline-allocation-2026-10-01.md).
The validation runner made no role, seed, schema, ledger or infrastructure changes.

## Exact run and outcomes

From the fixed worktree's `platform` directory, one command:

```text
go test -mod=readonly -json -count=1 -timeout=10m -p=1 -parallel=1 ./integration -run ^(TestDeliveryQueueWithSplitBotRole|TestCreditsRestrictedRuntimeGrants|TestCreditsMigrationAsRestrictedOwner|TestBrowserUsernameLookupKeepsBotOutsideCoreSQL|TestBotDeliveryReceiptDeniedBatchCannotStarveHealthyOwner|TestActiveEventSplitBotRoleResolvesThroughCore|TestRegistrationRetentionNativeImmutableAndMetadataRole|TestModernChoiceMealRuntime|TestModernChoiceLargeCreateEditReplace|TestDeletedPaymentCardKeepsSelectedLanguage)$
```

Native process session 43106; elapsed 167.5164893 seconds, exit 1. Local time:
`2026-10-01T03:39:30.1716818+02:00` to
`2026-10-01T03:42:17.6881711+02:00`.
The existing five-second assertions were unchanged. Test concurrency inside the
required scenarios was preserved; package/top-level scheduling was p1/parallel1.
The ten-case package had its own ten-minute budget and did not time out.

| Named integration test | Terminal result | Seconds |
| --- | --- | ---: |
| TestDeliveryQueueWithSplitBotRole | PASS | 1.41 |
| TestCreditsRestrictedRuntimeGrants | PASS | 1.01 |
| TestCreditsMigrationAsRestrictedOwner | PASS | 1.17 |
| TestBrowserUsernameLookupKeepsBotOutsideCoreSQL | PASS | 1.03 |
| TestBotDeliveryReceiptDeniedBatchCannotStarveHealthyOwner | PASS | 8.29 |
| TestActiveEventSplitBotRoleResolvesThroughCore | PASS | 1.18 |
| TestRegistrationRetentionNativeImmutableAndMetadataRole | PASS | 1.79 |
| TestModernChoiceMealRuntime | PASS | 7.76 |
| TestModernChoiceLargeCreateEditReplace | PASS | 59.34 |
| TestDeletedPaymentCardKeepsSelectedLanguage | FAIL | 2.15 |

All ten selected tests have terminal events; no nested test events or explicit
test skips occurred. The integration package terminal event is FAIL.
There were no reruns, new test timeouts, assertion changes, suppressions or source
edits. In particular, the large-choice case completed with its own focused
package budget; run09's global integration timeout was not treated as evidence
of an individual large-choice hang.

## Reproduced payment failure

`TestDeletedPaymentCardKeepsSelectedLanguage` failed at
`platform/integration/payment_instructions_test.go:98`:

```text
Received unexpected error:
order_not_found
```

The immutable 5057 source does not include the pending payment-redaction fix.
This reproduction neither evaluates that candidate nor establishes the precise
cause of the failure. Exact original events and the focused payment failure
extract are retained for a separately assigned investigation/review.

## Input integrity and retained evidence

All 2205 tracked platform/importer input files and all 88 SQL migration bodies
have identical actual SHA256 inventories before and after. Each actual input
inventory also exactly matches an immutable raw Git export of 5057: no newline
normalization or ledger rewrite occurred. Raw Git blob identity inventories are
identical. The fixed worktree's tracked Git status is clean.

| Inventory | Same before, after and raw Git SHA256 |
| --- | --- |
| Actual source | `8E0C2753CDD64DDB1A299D872081D88CF48CC5CD1A90D51C9258669D00D7761A` |
| Actual migration bodies | `F884DC5EA18A66FA72E0138CD2A8B6CC82CF78917C89E342F1D409D1CC8EA73A` |
| Git blob identities | `8F12176565D82AEF0ABE6FB085C2CA6CA319BEDD4608318BF8CDCC0B77D79FEB` |

Ignored evidence directory:
`C:/Users/ddriz/Projects/zns-chatbot/.worktrees/native-baseline-10-focused/qa.local/focused-10/`.
It contains `focused.jsonl`, `focused.stderr.log`, `receipt.json`, `summary.json`,
`payment-failure.log`, `roles-before.json`, role-helper stderr,
source/migration/Git inventories for before/after/raw-git, and the raw Git ZIP.

Raw `focused.jsonl` SHA256:
`F98722FA15688754C913B6DBA63763E10387FD6EF7BDE90877CA2E3504CD3B6B`.
Both role-helper and focused-command stderr logs are empty. Credentials are
absent from the public report and raw test command line.

No full platform/importer, browser/Telegram-like UI, live Zitadel/provider,
JavaScript, race, lint, or full migration acceptance checks ran here.
The stand's absent vector extension and absent zns_app role remain outside this
selected scope, not accepted. The prior 09 failure/timeout evidence remains intact.

Both the focused command and integrity capture are terminal. The runner releases
exclusive 58441 test/data ownership to root/setup lead, with no extra cleanup,
seed, role repair, restart or rebuild.

Written by baseline_09_runner (gpt-6/Codex)
on behalf of Daniel Drizhuk
