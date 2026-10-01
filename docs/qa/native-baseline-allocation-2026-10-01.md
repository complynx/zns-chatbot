# Native baseline PostgreSQL: actual allocation and setup release

Setup complete and released by `/root/fqa_lead` on 2026-10-01. Assigned runner:
`/root/baseline_09_runner`, immutable source
`0070f944691050677cd915a215e4ac0beea69fb6`. This is schema 089 intermediate
regression scope, not final 090, live runtime or migration Functional QA acceptance.
No broad tests were started by the infrastructure owner.

## Actual resources

| Resource | Actual handle |
| --- | --- |
| Compose project | synthetic-qa-zns-native-baseline-20261001 |
| Container | synthetic-qa-zns-native-baseline-20261001-postgres-1 |
| Container ID | 460a957f90b99e718b2bf340d4a21b4103cd250afad5cba37c9bc2623486955d |
| Volume | synthetic-qa-zns-native-baseline-20261001-pgdata |
| Network | synthetic-qa-zns-native-baseline-20261001-host |
| Network ID | 2f7f60a2d0d98765e983efa17795136af78c4123c468193b1dc0adf6de07dc09 |
| Native endpoint | 127.0.0.1:58441 |
| Initial database | synthetic_qa_zns_native_baseline |
| Cluster-local test owner | postgres; CREATEDB and superuser confirmed |

Actual `Config.Image`, ImageID and RepoDigests:
`postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24`.
Container is running/healthy. Host binding is only 127.0.0.1:58441 → 5432.
Dedicated bridge has only this container attached. Limits: 2 CPUs, 1GiB memory,
256MiB shared memory; restart policy no. Volume/network labels identify synthetic
native-baseline purpose and setup owner. No old stand/network/database changed.

## Preflight evidence

Fresh exact port/container/volume/network collision checks passed before creation.
Existing cached pinned image was used with pull never. Native Windows pgx/v5
connected over TCP 127.0.0.1:58441 and verified PostgreSQL 17.11/170011.
Created exact owned `synthetic_qa_zns_native_baseline_preflight_20261001`, connected
to it, dropped it, then verified absence. Helper closed its connections; zero other
client backends were observed before release. No product migrations or fixtures
were applied by setup.

Installed extension: plpgsql 1.0. There are 59 available extensions; vector is absent.
This matches the cached native developer image, not pgvector acceptance. Preserve
missing-extension failures/skips honestly; do not alter the image for green.

Client runtime: go1.27.0 windows/amd64. Exact preflight source:
`qa.local/native-baseline-20261001/preflight.go`, raw SHA256
`DA60E127C24AB696A3E223B3A5353C2FD74D78D6307C3AE514D000DC74B2E732`.
Go cache was isolated in the same ignored evidence directory. Connection failures
do not print raw DSNs. Receipt files:

- `qa.local/native-baseline-20261001/preflight-summary.json`.
- `qa.local/native-baseline-20261001/resource-receipt.json`, SHA256
  `CF99A606A3F0C7C717D8905B552CC5AD3251D542AA01BAFDDA41201F802EACE8`.
- `qa.local/native-baseline-20261001/database-readback.json`, SHA256
  `705E23B45FF29489A7C5BDDA4CBFE8D7A2EEFC429B2F088B80AA5666993476A8`.
- `qa.local/native-baseline-20261001/source-inventory-0070.json`, 2200 files,
  SHA256 `5c997d1f6eaac57151bcc4d670c35590761bebecf52b220fad8b2e685bb6cadc`.
- `qa.local/native-baseline-20261001/raw-migration-inventory-0070.json`, 88 SQL
  bodies, SHA256 `7ae2366bfac89b5933dd71f28694fb6e89799d92495f3b961d54c74109bf2254`.

Source inventories hash actual committed blob bytes via `git cat-file --batch`;
no decoding/newline normalization is used for body hashes. They cover platform
and importer modules at 0070. Runner must additionally capture its actual worktree
source/raw migration byte inventories before and after testing; committed inventory
is not a substitute for proving its test input remains unchanged.

## Private credential handoff

Ignored local file:
`C:/Users/ddriz/Projects/zns-chatbot/qa.local/native-baseline-20261001/private-owner.json`.
SHA256 `08D2B2F1CA73BFAC1374A4AA90087C2B3A40DCA5CE87FA8B5CF8C8958C3ACE44`.
ACL inheritance is disabled, with one full-control rule for its current owner.
Password/DSN are not present in this report, tracked configuration or tool output.
The runner reads the bundle privately at runtime and sets `TEST_DATABASE_URL`
without printing it. Do not expose the bundle through screenshots/logs/Git.

## Exclusive ownership contract

Setup ownership is released. `/root/baseline_09_runner` is now the sole test/data
writer for this cluster, using its own worktree/caches and root's p1/parallel1
full native package run plus separate importer scope. Setup writer pauses all
infrastructure mutations until runner explicitly releases. No restart/rebuild,
reseed, cleanup or extra consumer is permitted during the run.

Only this cluster's disposable databases may be changed. Developer 55432 and
ce427 flows/recovery remain separate and preserved. Test-generated random
databases must retain their normal cleanup semantics. Failure reproduction
resources stay until no pending check depends on them. Runner does not edit
product code or weaken gates. Skipped/unsupported live UI/Zitadel/provider/race
checks are not passed; this native baseline does not establish full migration.

Reproducible secret-free configuration:
`docs/sandbox/native-baseline/compose.yaml`, SHA256
`FF1B5BBD7B77368F8EA5FECB57E4449C5ED44A234B7C3B882F0E57F5631D278A`.
External network/volume must never be recreated over active runner resources.

## Deferred role bootstrap for a fresh baseline

Frozen run09 remains unchanged. Root identified that the new cluster did not
receive the existing `platform/sandbox/roles.sql` prerequisite: `zns_api`,
`zns_bot`, and `zns_meter`. Conditional migrations do not create these roles.
This setup gap is recorded separately from product defects; preserve run09
failure evidence and wait for the runner's explicit terminal release before
any role, database or infrastructure write.

For fresh run10, root assigns its immutable epoch and a single setup writer.
Record raw source/hash of that epoch's `platform/sandbox/roles.sql`, inspect
existing cluster roles, and apply only the explicitly reviewed missing-role
bootstrap to this exact58441 cluster. The script also creates database-local
core/bot schemas and default grants; do not blindly rerun the entire non-idempotent
script over surviving test databases. Capture exact role and database/grant
readback and release setup to a separately assigned runner before broad tests.
No copied developer database, old-ledger reset or frozen-run patch is permitted.

Managed FQA `product-roles.sql` is a separate prerequisite: its zns_app/zns_meter
ownership and core/bot/interaction grants differ from the native api/bot split.
It is not a replacement for the native roles bootstrap. Assess zns_app needs
against the fresh baseline's actual test scope and immutable fixtures before
adding a narrowly reviewed prerequisite; do not import a managed installation
or app credentials into the baseline. Product payment `order_not_found` is a
separate reported failure requiring isolated reproduction; missing-role bootstrap
does not establish its cause or resolution.

## Released run09: roles-only bootstrap completed

Root confirmed run09 fully terminal and released the cluster to the lead. The
lead verified the exact container/digest/loopback58441 binding and zero other
client backends. All four queried roles (zns_api, zns_bot, zns_meter, zns_app)
were absent. Root authorized only the first three CREATE ROLE definitions from
immutable `5057ddb0547f5b099459fe3d321c04a7c4490c90`:
`platform/sandbox/roles.sql`, raw blob SHA256
`859BB43EF1E62A7167F0253DBF0D1A818F77ABD620F04B10AE0491D3744F43DB`.

Those three definitions were applied transactionally through psql stdin on the
exact owned container. Readback: zns_api, zns_bot and zns_meter have LOGIN and a
stored password; all have superuser, CREATEDB, CREATEROLE, replication and bypass
RLS false. zns_app remains absent. No schema/default-grant section was applied.
Before/after database names and the baseline database's schema/owner inventory
are identical. Surviving run09 databases, failure/timeout logs and ledgers were
preserved. No full baseline10, affected-case tests or meal reproduction launched.
Developer55432 and ce427 were not accessed or changed.

Actual endpoint remains `127.0.0.1:58441`; native TCP connection succeeded.
The mutation transport was owned-container `docker exec` PostgreSQL psql stdin.
Private owner bundle/hash remain unchanged as documented above. The exact three
synthetic role definitions are in owner-only ignored
`qa.local/native-baseline-20261001/roles-bootstrap-5057/private-role-definitions.sql`,
SHA256 `4CC309D557F0526E7599FC332ECFC8C4D6D2E1FF3E5F7FED4344905E8EDE4265`.
No credentials are included in this report.

Terminal readback receipt:
`qa.local/native-baseline-20261001/roles-bootstrap-5057/receipt.json`, SHA256
`C689D3BBFE39F6F11A40CBE54C384EA5F420D12DF6F8ACF3D473199667F48F0C`.
The setup writer `/root/fqa_lead` releases exclusive infrastructure ownership
back to root. Freeze the corrected role prerequisite before root assigns any
next test writer; do not independently restart/reseed or launch baseline10.

## Subsequent focused10 and planning ownership

Focused10 is terminal and released: nine PASS, one FAIL, zero SKIP and zero
unfinished across its exact ten tests, at source5057. Seven role cases and both
modern-choice cases passed; payment card order_not_found reproduced. See
`docs/qa/composition-baseline-2026-10-01-10-focused.md`. This does not replace
run09's full-suite failure/incomplete coverage or explain its meal-runtime failure.
Root also confirmed the role-controls developer's assigned native PG work finished
and released58441. No new writer is inferred; lead performs planning only until
root assigns another exact exclusive window. No role/DB/resource changes here.

Final broad suite shard/allocation proposal is in
`docs/qa/fqa-lead-2026-10-01-next-batch.md`. Proposed extra58451 cluster is only
reserved pending infrastructure assignment; it has not been created or started.

Root subsequently approved only the concrete config preparation:
`docs/sandbox/fqa-stands/native/shard.compose.yaml`. It reserves the extra58451
cluster with the same pinned digest and2CPU/1GiB/256MiBshm, external separate
volume/network, pull never and restart no. Runtime credential variable is
NATIVE_SHARD_PASSWORD; private bundle is a future owner-only ignored file, not
created by config preparation. No resources, bootstrap or tests are started.
Final immutable source, writer and role prerequisite manifest remain separately
assigned. Full acceptance still requires actual Go-discovered union and complete
unfiltered other-platform/importer coverage, not source-scan estimates.

Config syntax check returned exit0; runtime readiness is not claimed. Compose
raw SHA256 `3C86799B628CD21E3B710FD45E10F8D1C7468DE34F868CE476DE21F9B82687BC`.

## Actual second native shard: prepared idle

Root subsequently authorized exact second-cluster setup, with the lead as sole
infrastructure writer. Fresh port/resource collision checks found no conflicts;
cached RepoDigest matched. Docker reports32CPUs and63,006,920,704bytes memory;
observed existing containers left capacity for the bounded2CPU/1GiB cluster.
Only the following resources were created; no other stand or test/source changed:

| Resource | Actual handle |
| --- | --- |
| Project | synthetic-qa-zns-native-shard-20261001 |
| Container | synthetic-qa-zns-native-shard-20261001-postgres-1 |
| Container ID | 2516c2ca9430fbe7e72d7706aadb52f9c2a16a6586b59f8dc66d660c6d4dc1ec |
| Volume | synthetic-qa-zns-native-shard-20261001-pgdata |
| Network | synthetic-qa-zns-native-shard-20261001-host |
| Network ID | 1447d75667ca9c56837af340c196dc3666f0fe07ee066ce36c3ffb82a5fa6ab7 |
| Native endpoint | 127.0.0.1:58451 |
| Database | synthetic_qa_zns_native_shard |

Config.Image, ImageID and cached RepoDigests all match the existing pinned
postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24.
Actual limits2CPU/1GiB/256MiBshm, restart=no, loopback-only58451 confirmed.
Dedicated labeled network contains only this container. Container is healthy.
Native Windows pgx authenticated over TCP and read PostgreSQL17.11, exact
database/ownerpostgres and the required role state. Setup created no extra
disposable database. No product migrations, fixtures, grants, schemas or tests
were applied; migration ledger absent and no other client backends at readback.

Only first-three CREATE ROLE definitions from exact assigned product source
`56ac8b79cee049bb6108fc47af916aa8912af80d` were applied transactionally after
verifying absence: zns_api/zns_bot/zns_meter, LOGIN/password present, superuser,
CREATEDB, CREATEROLE, replication and bypassRLS all false. zns_app remains absent.
Raw roles.sql SHA256 remains
`859BB43EF1E62A7167F0253DBF0D1A818F77ABD620F04B10AE0491D3744F43DB`.
Database/schema inventory was unchanged by roles-only bootstrap.

Private owner bundle: qa.local/native-shard-20261001/private-owner.json, SHA256
`240ED22C24A57EFCC031BFFC3F2F425D67803F8EF7617E87C151E3275A42066A`.
Ignored by Git; ACL inheritance protected, one owner-only FullControl rule.
Generated password passed only through runtime environment to Compose; no
credential argv/log/tracked config. Existing role passwords came directly from
the exact raw source statements through private psql stdin, not printed.

Evidence in qa.local/native-shard-20261001:

- setup-receipt.json SHA256
  `3ABF116379F53444A9EFE7868A8C4C0F3E08709BB3C02FCC5307CA47154CF87F`.
- native-readback.go SHA256
  `4552249B94D3E627AAE47E65C10AE24F2C057A591B753EBB50F73A2CF8EFC958`;
  native-readback.json SHA256
  `D9BD9C4B9E3B6B262874498968ABB121E46B58E2891FDECA6D17CFF5209BB4AF`.
- source-inventory-56ac.json:2209 raw committed files, SHA256
  `93624586b3791250c7751a6af8e8bfb8fc62e4d277750d36bd8e164ba3c04ab2`.
- raw-migration-inventory-56ac.json:89 unnormalized raw migration bodies, SHA256
  `57e59d9d446fabc936e2187e8d382f787d6172cc795a1b18f584876538027906`.

Assigned source's last migration is090_pass_delivery_targets.sql. Its raw bytes
are inventoried above; schema090 is not applied to the prepared cluster.

These source/schema inventories identify the setup provenance only; no source
test or migration execution/final freeze is certified. Final reviewed source,
migration application and test writers require separate root assignment. Setup
is terminal; lead releases exclusive preparation ownership to root, preserving
the healthy idle cluster. No restart/reseed/rebuild or unassigned consumer while
awaiting that assignment. Developer55432, baseline58441 and ce427 were untouched.

## Integrated role-control checkpoint

QA255 independently passed the five-path fixed-role candidate67206220. Root
merged it without conflicts as102072a0; focused sandbox/command tests passed.
The four fixed actions are now integrated source, still absent from sealedce427
images. Template preparation and future stand installation do not certify FQA.
