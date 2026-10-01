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
