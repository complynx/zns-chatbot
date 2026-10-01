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

## Private-role suite remains separately allocated

Reviewed bd150 runtime14 adds TestRegistrationPrivateRoleComposition requiring
five private REGISTRATION_ROLE_TEST_*_URL endpoints: ADMIN/OWNER/OPERATOR/FAKE/
INVENTORY. It changes cluster role memberships/privileges and DB/table ownership
to prove guard rejection. Native58441/58451 receive neither private roles nor
fixture data. Proposed fresh58461 cluster owns this suite; separate proposed58471
owns the original registration integration case that DROPs schemas/fixture/
migration tables. No preparation or resource action was taken by this note.

Next-batch plan records exact environment/owner/ACL/raw runtime-roles file/layout
requirements, original package/shard membership and Linux/mount/provider/live
stand prerequisites. Full unfiltered other-platform, eight integration manifest
union and importer remain covered through final Go discovery plus qualified
supplemental receipts; no silent skips or source-scan-only acceptance. All future
special-cluster creation/role bootstrap and writers require explicit assignment.
Prepared native parents, developer55432 and ce427 remain preserved. This is
planning, not role composition execution or FQA acceptance.

## Actual bare special clusters58461 and58471

Root explicitly authorized both separate clusters; lead was sole setup writer.
Fresh native ports and full Docker name/network/volume inventory had no conflicts;
cached RepoDigest matched. Only these projects/resources were created through
their ignored local Compose files, with no other stand or shared role mutation.

| Handle | Role composition58461 | Destructive fixture58471 |
| --- | --- | --- |
| Project prefix | synthetic-qa-zns-registration-role-test-20261001 | synthetic-qa-zns-registration-fixture-test-20261001 |
| Container suffix | -postgres-1 | -postgres-1 |
| Container ID | f39280bdf0010a672baeed1e89ec8cadf6114f129206648873fd67723d5f25de | 5fb9ae2d49552c1a31a17084e180fb2963dc5b5d791aa638b3de135a348bb892 |
| Network suffix | -host | -host |
| Network ID | 01d972ed842967ef97e10e6eb1c106a84b1e9837c04cd90c1e20a597768f2321 | f0a5a88e7ed5a53b628a24c17ef90dae17298c8c61bab3bfaceb0a9b4645a0a4 |
| Volume suffix | -pgdata | -pgdata |
| Native endpoint | 127.0.0.1:58461 | 127.0.0.1:58471 |

Each prefix+suffix is its actual full resource name. Each network contains only
its own container; pgdata is the sole RW mount at /var/lib/postgresql/data.
Labels: synthetic=true, owner=fqa_lead and registration-role-test or
registration-fixture-test purpose. Each actual Config.Image/ImageID/RepoDigests
matches postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24;
limits1CPU/768MiB/256MiBshm, restart=no, loopback-only publication. Both healthy.
Database name on both distinct servers: synthetic_qa_zns_registration_fixture,
owned by nonprivileged zns_app. Native authenticated pgx ADMIN and OWNER reads
verified PostgreSQL17.11, current_user postgres/zns_app respectively, exact
endpoint and DB owner. Product schema count and public table count both zero.

58461 bare roles: zns_app, zns_meter, zns_fake, zns_registration_operator,
zns_inventory. All LOGIN; superuser/CREATEDB/CREATEROLE/replication/bypassRLS false.
Inventory alone has pg_read_all_stats membership and default read-only; app/fake/
operator have no memberships.58471 bare roles: zns_app,zns_api,zns_bot,zns_meter,
same unprivileged LOGIN flags. Database CONNECT grants and owning app role were
bootstrapped only. No product migrations, schemas, data, fixture/clock markers,
runtime ACL SQL or tests were applied. No other clients at final readback.

Private bundles (each ignored, protected ACL, exactly one owner FullControl rule):

- qa.local/registration-role-test-20261001/private-owner.json, SHA256
  `3CDAC901F4921316D4E42D0C11B61C7E6D2ECCE9F8A6648B5E7CE713A3EE92BA`.
- qa.local/registration-fixture-test-20261001/private-owner.json, SHA256
  `9106E9935F7E15BB5C8EE2195E98728CF630058AD0CA869462D9DBC697541C37`.

Format KEYS ONLY: origin,host,port,database,credentials,urls. Credentials keys
include postgres plus the exact respective bare roles above. Role URL keys:
ADMIN,OWNER,OPERATOR,FAKE,INVENTORY; fixture URL keys: ADMIN,OWNER,API,BOT,METER.
All credentials newly generated independently by the lead this setup with local
cryptographic randomness, synthetic only; no external credential source. Admin
secret enters Compose runtime environment, role definitions private psql stdin;
no password argv/output/Git. Setup approved command was
qa.local/registration-prerequisites-20261001/setup.ps1, require_escalated,
terminal session78410. Bootstrap/source inspection epoch is bd150d5b; this is
bare-role preparation, not final product source/migration execution authority.

Public receipts: respective qa.local/registration-*-test-20261001/setup-receipt.json,
SHA256 role `4823ACAEA1975D88870B44B1A063F271C009E1A0118120CD88CA16F829D65480`,
fixture `011100D081FD187BDB0743D3DB3A7CD76037521A057368F1307C24A40052A884`.
Complete sanitized precreation inventory and native JSONL readback are under
qa.local/registration-prerequisites-20261001. Helper raw SHA256
`02520DCE8F427AB71999EBC8FE6D7E31C7309E4D6D98B28E5A28CF4F4841538D`;
setup script raw SHA256
`220B9297CFD0780E20EF2504B9562EF6BBAABD5E7F9541CC0FD2194F77FCF936`.

Safe root-owned runtime readback invocation, from platform directory, after
coordination with the assigned writer: set REGISTRATION_PRIVATE_ROOT to the
absolute repository qa.local directory; own GOCACHE/GOWORKoff/local Go/readonly
modules; `go run ../qa.local/registration-prerequisites-20261001/native-readback.go`;
remove the private-root setting afterward. This previously approved helper reads
only its own two newly generated synthetic bundles, validates exact endpoints,
and outputs ownership/count/version metadata, never credentials. It is not a
secret-printing probe or permission to target another cluster. Test assignment
will map private URL keys into the exact environment settings separately.

Setup terminal; lead releases both clusters to root, preserving healthy idle
bare state. Root assigns final immutable codegate/test windows before migrations
or fixture/test writes. Lifecycle remains lead-managed: no unassigned restart,
rebuild, bootstrap or consumer.58441/58451, retained fixture DB, C204, developer
55432 and ce427 were untouched. This is readiness of bare prerequisites only,
not private-role test PASS, Functional QA or final acceptance.

Final shard orchestration is now specified in the next-batch plan: actual
per-package Go-listed runnable names, ordinal sorting/index modulo8, complete
module/package/OS unions and pairwise disjoint manifests. Native A owns58441/even
shards/full other-platform plus role61; B owns58451/odd shards/full importer.
Original fixture71 ownership goes to its actual discovered shard owner only in
an explicit window. Provider native8090 is a single lead-controlled reservation;
Linux/RO-RW/provider/lifecycle qualifications need separate scoped runner and
stand contracts. No final manifests/source/writers are certified from the current
checkpoint. No tests or database/credential/resource changes were performed for
this orchestration note.

Actual checkpoint187319ce Go list-only metadata found platform2187 runnable
parents/63 test-bearing packages, integration1060 and importer136/2. Current
ordinal modulo8 preliminary shard hashes and exact special selectors are recorded
in next-batch plan. No test functions, database/credentials or resource lifecycle
actions were invoked; only metadata compilation used the lead's existing cache.
Final source remains unfrozen and requires new full discovery before assignments.
