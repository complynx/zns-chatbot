# Native Go/PostgreSQL composition baseline 09

Terminal result: **FAIL; incomplete integration coverage**. The full platform
command exited 1, and its integration package hit the assigned 45-minute timeout.
The separate importer command exited 0 with one explicitly skipped Windows test.
This is regression evidence for intermediate schema089 source, not final090,
Functional QA acceptance, release acceptance or production authorization.

## Fixed inputs and ownership

- Source: `0070f944691050677cd915a215e4ac0beea69fb6`.
- Branch/worktree: `codex/native-baseline-09`, `.worktrees/native-baseline-09`.
- Native runtime: `go1.27.0 windows/amd64`; `GOWORK=off`,
  `GOMAXPROCS=2`, `GOTOOLCHAIN=local`, `GOFLAGS=-mod=readonly`.
- Owned build cache and evidence: worktree `qa.local/baseline-09/` (ignored).
  Go read the existing native module cache. No default configuration changed.
- Sole database/test writer: `/root/baseline_09_runner`, after explicit setup
  release from `/root/fqa_lead`. Allocation and preflight are recorded in
  [the stand allocation report](native-baseline-allocation-2026-10-01.md).
- Exclusive synthetic PostgreSQL 17.11 endpoint: `127.0.0.1:58441`.
  Private bundle SHA256 verified before tests:
  `08D2B2F1CA73BFAC1374A4AA90087C2B3A40DCA5CE87FA8B5CF8C8958C3ACE44`.
  Credentials were read privately and were not printed or tracked.

There were no source edits, Docker lifecycle operations, image changes, reseeds,
stand repairs, role bootstrap, migration ledger rewrites or newline normalization.
No developer55432, ce427, Functional QA or production cluster was accessed.
Tests retained their own transaction/concurrency scenarios and normal disposable
database cleanup. Package/top-level parallelism was throttled as assigned.

## Commands and terminal evidence

From the fixed worktree's `platform` directory:

```text
go test -mod=readonly -json -count=1 -timeout=45m -p=1 -parallel=1 ./cmd/... ./internal/... ./integration/... ./identityprovision/... ./deploy/...
```

Started `2026-10-01T00:21:15.7414772Z`, ended
`2026-10-01T01:19:02.0365286Z`; exit 1, elapsed 3466.2950514 seconds.
This includes compilation into the empty owned build cache. Integration started
at `02:33:49.533444+02:00` and timed out at `03:18:49.7983696+02:00`.
Identity-provisioning and deployment packages still completed afterward.

Then, serially, from `tools/migrate`:

```text
go test -mod=readonly -json -count=1 -timeout=15m -p=1 -parallel=1 ./...
```

Started `2026-10-01T01:19:02.1165354Z`, ended
`2026-10-01T01:22:54.9585878Z`; exit 0, elapsed 232.8420524 seconds.
Both commands ran once in native process session 66624; no selected reruns.

| Scope | Test-event PASS | FAIL | SKIP | Package PASS | FAIL | No-test SKIP |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Platform cmd | 103 | 0 | 0 | 3 | 0 | 6 |
| Platform internal | 3428 | 0 | 21 | 57 | 0 | 16 |
| Platform integration | 561 | 9 | 5 | 0 | 1 | 0 |
| Platform identityprovision | 15 | 0 | 1 | 1 | 0 | 0 |
| Platform deploy | 1 | 0 | 0 | 1 | 0 | 0 |
| Platform total | 4108 | 9 | 27 | 62 | 1 | 22 |
| Importer | 366 | 0 | 1 | 2 | 0 | 0 |

Test-event counts include nested subtests. Platform top-level terminal events:
1641 PASS / 9 FAIL / 27 SKIP. Importer top-level terminal events:135 PASS / 0 FAIL / 1 SKIP.
Package no-test skips are separate from explicit test skips.

## Observed failures and unfinished coverage

Seven integration failures stopped on absent cluster roles. The fresh stand did
not execute `platform/sandbox/roles.sql`, which normally creates `zns_bot` and
`zns_api` through Docker initialization. These are fixture prerequisite failures;
this frozen run did not repair them.

| Failed test | Actual error |
| --- | --- |
| TestDeliveryQueueWithSplitBotRole | `zns_bot` absent, SQLSTATE 42704 |
| TestCreditsRestrictedRuntimeGrants | `zns_bot` absent, SQLSTATE 22023 |
| TestCreditsMigrationAsRestrictedOwner | `zns_bot` absent, SQLSTATE 42704 |
| TestBrowserUsernameLookupKeepsBotOutsideCoreSQL | `zns_bot` absent, SQLSTATE 42704 |
| TestBotDeliveryReceiptDeniedBatchCannotStarveHealthyOwner | `zns_bot` absent, SQLSTATE 42704 |
| TestActiveEventSplitBotRoleResolvesThroughCore | `zns_bot` absent, SQLSTATE 42704 |
| TestRegistrationRetentionNativeImmutableAndMetadataRole | `zns_api` absent, SQLSTATE 22023 |

Two additional failures remain separate and unclassified:

- `TestDeletedPaymentCardKeepsSelectedLanguage`: unexpected `order_not_found`
  at `platform/integration/payment_instructions_test.go:98`.
- `TestModernChoiceMealRuntime`: `Condition never satisfied` at
  `platform/integration/modern_orders_choice_runtime_test.go:261`, called from
  `modern_orders_meal_runtime_test.go:35`. The log preserves runtime diagnostics
  and targeted goroutine stacks. Observed stages were `order_event`,
  `script_runs`, `input`, `orders_reply`, `reply_origin`; durable turn state was
  derived/ready, inbox 1, telegram cursor 0, received 2. No causal diagnosis or fix
  was performed by this validation runner.

The integration package emitted `panic: test timed out after 45m0s`.
`TestModernChoiceLargeCreateEditReplace` was the running test, for only 5 seconds
when the package-wide alarm fired. This is not evidence of a 45-minute hang in
that individual test. The timeout left 1131 announced test/subtest entries without
terminal events, including 472 top-level tests. They are **unfinished**, not PASS
or SKIP. The full list and timeout stacks remain in ignored evidence.

## Explicit skips and limits

The 27 platform test skips comprise:

- Three real-model tests requiring `ZNS_LIVE_CODEX_EXECUTABLE`.
- One local Zitadel adapter test requiring `ZITADEL_LOCAL_STATE`.
- Ten media tests requiring Linux ffmpeg/ffprobe or a POSIX fake probe.
- One Linux deployment-lock test, one isolated Unix script-worker test,
  and two Linux child-reaping tests.
- Three native sticker-renderer/decoder tests requiring their contained images.
- Five integration stand tests: live sandbox, browser-auth UI, physical Linux
  runtime replacement, dedicated registration-fixture database, contact browser.
- One SDK provisioning test requiring its local Zitadel proof state.

Importer skipped `TestSnapshotSymlinkIsRejected`: the Windows toolchain could
not create the symlink. Other Windows reparse tests ran; this skip remains a skip.
Exact skipped test names and reasons are in the module summaries.

The pinned PostgreSQL image has 59 available extensions and installed plpgsql 1.0;
`vector` is unavailable. This run neither added extensions nor established
pgvector acceptance. Unfinished tests cannot supply extension proof.
JavaScript, live Telegram-like UI, live Zitadel/provider flows, race detection,
lint, dependency verification and complete migration parity were outside this
Go/PostgreSQL baseline. They are unsupported/not run here, never PASS.

## Source and migration integrity

Before and after inventories hash actual on-disk bytes with SHA256. All 2200
tracked platform/importer input files and all 88 SQL migration bodies were
unchanged. Each actual input hash also matched the corresponding committed raw
blob hash in the setup owner's immutable 0070 inventory: 0 differing file hashes.
Raw Git blob identity inventories matched before/after, and the fixed worktree's
tracked Git status remained clean.

| Inventory | Identical before/after SHA256 |
| --- | --- |
| Actual source | `3B6D7BF2E140A6645FBA5E689C1AD2FCA61FF365C5467A1769D08A2CA96615D9` |
| Actual raw SQL bodies | `F884DC5EA18A66FA72E0138CD2A8B6CC82CF78917C89E342F1D409D1CC8EA73A` |
| Git blob identities | `DE50D16314BA96BC8FD5AF3FF7570E56EABDE0FB4A7847AC22C86636C1FC161F` |

Detailed retained files under worktree `qa.local/baseline-09/`:
`platform.jsonl`, `importer.jsonl`, module stderr logs/receipts/summaries,
`terminal-summary.json`, `source-{before,after}.json`,
`migrations-{before,after}.json`, `git-blobs-{before,after}.txt`.
Fresh isolated investigations can read `meal-runtime-failure.log` for the exact
meal-runtime diagnostics/stacks and `modern-choice-diagnostic-events.jsonl` for
the corresponding meal-runtime and large-choice raw JSON events. The complete
original `platform.jsonl` remains authoritative for ordering and all diagnostics.
Failed logs, timeout evidence and any timeout-surviving disposable resources are
preserved. The runner performs no extra cleanup.

Terminal raw log SHA256:

- `platform.jsonl`:
  `D822BC1E1AB016D8713B86DFA06C71DD1814C872C75A2290AF3533268FFAA8FC`.
- `importer.jsonl`:
  `B6464946B7DF9B706F2079BF4A15D630F2EABAB0FAD4A1799159A74D1B797043`.

The runner releases exclusive test/data ownership after both commands and
integrity checks are terminal. Infrastructure ownership returns to the setup
lead; this release does not authorize altering another stand or production.

Written by baseline_09_runner (gpt-6/Codex)
on behalf of Daniel Drizhuk
