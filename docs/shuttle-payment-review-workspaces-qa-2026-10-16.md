# Payment review workspace store QA, 2026-10-16

The [workspace store](shuttle-payment-review-workspaces.md) is component-verified.
It is not a Mini App, new agent API or four-path user acceptance. Source schema 39
has not been installed on the owned stand; a direct read of its goose version
still returns 38. No production change, runtime cutover or publication occurred.

## Checks and evidence

The final Linux race gate used a new disposable PostgreSQL database and the same
payment/notification/authority regex as the prior transaction extraction:

```
go test -json -race -count=1 -p=1 -parallel=2 -timeout=180s ./integration ./internal/payments ./internal/api ./internal/db ./internal/task -run '^Test(.*Payment|.*Notification|.*Authority|PassDeposit)'
```

The exact expected set passed: 47 top-level tests and 69 pass events, with no failed
or skipped tests. Integration contributed 43 roots/60 events, payments two/two,
database one/six and task one/one. The API package compiled with no matching roots;
existing payment HTTP behavior is covered by the selected integration tests, not
by a new workspace HTTP endpoint.

Seven added store scenarios cover pending/no credit and corrected partial credit;
concurrent aliases and reopening the store; immutable prepare/cancel receipts;
edit/cancel blocking both new and existing domain entry points; exact parameter,
proposal and revision binding; expiry and historical replay; owner isolation and
role revocation; rejected proof without credit; rollback of ledger/notification/
workspace when the outer receipt write fails; and read-only operation recovery
after direct canonical completion. These overlap within the seven scenarios.
The new native browser/session/tool workflow is not exercised by these tests.

The gate froze 828 release/test inputs. Seven paths changed relative to the accepted
transaction-extraction snapshot: the workspace store, its integration tests,
source migration 39, payment core, SQL source and two generated Go files.
Final database/integration lint passed with zero issues. sqlc compile/generate passed;
regeneration left every generated Go output byte-identical. The current release/test
inputs still match the accepted final gate. The Git index stayed empty.

Five fresh read-only source reviewers across four diff versions found and closed:
a legacy domain-entry bypass of cancelled/superseded decisions, missing canonical
workspace completion, incomplete pending-operation recovery and status failing to
resolve completion through another interface. The final v4 review was clean.

## Retained failures and boundaries

The first gate failed to compile a test fixture literal. The second compiled but
failed two replay comparisons because equal instants had Local versus UTC locations.
Both raw test outputs, checkpoints and scoped lint failures remain under
.cache/shuttle-work/payment-review-workspace-qa and
.cache/shuttle-work/payment-review-workspace-v2-qa. New proposal/decoded outer expiry
instants now use UTC; strict replay comparisons remain. The intermediate v3 gate
passed 47 roots/69 events, then a later status recovery fix required the final v4 gate.
No failed gate has been relabelled as passed.

All owned transient containers and volumes from these four runs are removed.
Each Go container used a 2 GiB limit and PostgreSQL 512 MiB, with no additional swap;
no OOM occurred. The ten main stand containers retained their recorded IDs, images,
start times, restart counts and running state. This proves scoped test isolation,
not application restart acceptance or a production readiness claim.

The final local receipt is .cache/shuttle-work/payment-review-workspace-v4-qa/proof.json,
with checkpoint, exact expected roots, JSON test output, frozen inputs and lint logs.
Existing-data schema 38-to-39 clone rehearsal, image/runtime activation, Mini App
session mutation safety, forms, agent manifest, native context and real-service
four-path QA remain for the complete [reviewer slice](shuttle-native-payment-review-plan.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
