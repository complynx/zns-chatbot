# Next immutable Functional QA batch requirements

Planning only. Current ce427 flows/recovery batches are complete and released;
their source/images/state/evidence remain preserved. Import has not started.
Lead owns allocation/freeze/release and public reviewer handoff. Engineer may
mutate only the exact resources assigned for a reviewed next batch.

## Required composition

Root's planned batch includes reviewed callback-answer observation and genuine
C scenario fixtures, plus menu migration090 when its gates/review are ready.
ACK planning advanced: reviewed successor `62b9eebc` is locally merged; lead
verified root HEAD `6812e359b0e4338eed3e03eca5dbee71bc912b10` read-only.
This is current source state, not the final combined image epoch. Earlier
`acd6c12`/QA239 state is historical.
Other capability commits are not assumed integrated. Root supplies the final
full immutable Git ID and required gate results before build.

Keep unrelated D collector work outside the narrow ACK readiness dependency.
Any collector outcome remains a separate required D scope, not silently passed.
The original 40-row scenario matrix stays open according to actual coverage.

## Efficient build alternatives

### ACK-only evidence batch

If C/menu review waits materially, build one new fake image from the reviewed
ACK candidate, reusing the exact ce427 managed app/helper/coordinator/PostgreSQL
images. This can test actual callback endpoint response observation promptly.
Its provenance is explicitly composite: ce427 application plus reviewed ACK
provider, not one current-product-source epoch. It cannot accept current C/D/E
or current schema090 behavior.
Before selecting this route, verify the current fake's startup and data contract
against the retained ce427 database schema without applying newer migrations.
If the provider requires newer product schema/state, use the combined batch or
an independently reviewed compatible provider composition; never silently upgrade
the retained app/database to make an ACK-only claim.

Preparation must use a separate isolated clone or a root-approved exclusive
reuse/snapshot window. Preserve old provider/state/journal evidence. A provider
restart changes process-local receipts and loses model fixtures; never fabricate
fixture restore or claim restart durability from process-local ACK records.
Document exact new provider/config/volume/network/resource identities and scope.
Do not replace the original released fake and silently relabel ce427 evidence.

### Combined ACK + C + current-schema batch

Preferred when reviews are ready together: use one exact final Git export for
current app/fake, genuine fixture setup, SQL migrations and any E CLI artifacts.
Build the zns binary/app/fake from that export. Helpers/coordinator may be reused
only if verified compiled inputs, actual binary bytes and actual image bindings
prove they are unchanged; a tag or source-package name is not preservation proof.
Changed helpers must be rebuilt. This avoids rebuilding all six unnecessarily
without pretending old compiled code came from the new source.

Reuse does not skip managed replacement: record actual old/new role containers,
generation/launch IDs, admission/writer/helper-session retirement, image refs
and new runtime bindings. Only the supported owner starts/replaces the group.

## Build and data proof

1. Freeze final reviewed commits and a clean archive/source manifest. Do not use
   dirty worktree composition or normalize sources for hashing.
2. Migration inventory hashes the raw file body bytes consumed by the migrator.
   Keep raw bytes unchanged, including line endings and BOM if present. Do not
   compare normalized text checksums to the database's raw migration hashes.
3. E source export, CLI and source/schema contract come from the same final Git
   export. Record CLI build hashes, inventory/counts and actual raw migration
   versions/hashes. Old 089 rehearsal is not 090 acceptance.
4. Record actual compiled artifacts and image RepoDigests. Runtime `Config.Image`
   must use resolvable `repository@sha256`; actual ImageID/RepoDigests and in-image
   binaries must match the prepared manifest.
5. Allocate separate mutable namespaces/volumes/config/provider cases/installations,
   grants/test users and journals. Root PostgreSQL 55432 remains outside FQA
   resource ownership. Preserve ce427 evidence; no concurrent scenario writers.
6. Seed genuine C data through actual product validation/transactions: event A/B,
   sales/capacity/pair/roles/tiers, ordinary/event-payment/global-admin-without-
   payment-role identities, live grant/revoke/consent, private knowledge/source
   markers, registration ingress/draft/rank/deadline and replay identity. Missing
   product capability belongs to root developers; never implement fake success.
7. Publish only public access/requirements/expected fixture constraints to fresh
   reviewers. Lead engineering evidence and earlier findings stay separate.

## Lead readiness and acceptance split

Before freeze, inspect actual UI and controls in an ephemeral real browser,
actual image bindings, current migration inventory, mutable-state isolation and
exclusive ownership. ACK public readback must expose actual authenticated callback
answer response generation, scoped to the right synthetic user, bounded and
redacted. It is process-local provider observation, not proof of real Telegram
or client receipt. A card edit alone remains insufficient.

Genuine C scope requires EN/RU manual→agent→manual registration/knowledge,
current discovery/execution denial, event A/B role distinctions, live revocation,
privacy/deletion, pending-intent escape and bounded/interrupted state. Registration
queue/order scenarios need canonical actual ingress and draft/rank/deadline
readbacks; ordinary generic booking versions are not registration evidence.

Recovery needs exact documented supported owned faults: admission-session loss,
SQL contention/outage, signals/crash/drain, helper termination and physical managed
replacement. Preserve fake lifetime for selected edit uncertainty scenarios.
The consumed model-turn before durable plan-save and provider-reboot case remains
required; nonreplayable fixture behavior is an uncovered gate, not permission to
reinsert a consumed step or invent a winner. Keep unsupported outcomes explicit.

Commission fresh source-blind reviewers after substantive changes. They receive
requirements/scope/public access only. Older reviewers' continued execution cannot
substitute for a fresh affected independent acceptance gate. Freeze prevents
rebuild/reseed/restart except the exact allocated recovery fault window; release
precedes any new engineering mutation. Scoped success never closes the whole stage.

## Real integrations: decisions versus engineering

The existing recorded plan names CLX Test Bot, Telegram ID6087685431
(`docs/code-quality.md`, real Telegram section), and permits interactions only
from Daniel's designated test account after main synthetic gates. It does not
authorize unrelated recipients or production cutover. The audit records that
Daniel will rotate the test bot token himself; agents do not rotate it. Do not
read/copy its token for this planning task.

No exact designated test-account/recipient identity was found in the current
read-only plan. Confirm that identity and its scope only when the real gate is
ready, or use existing trusted direct authorization if root already has it.
This is the genuine human contract dependency; there is no reason to ask Daniel
to choose another bot or decide routine image/network/test setup. Real model
checks already have bounded-cost authorization after main synthetic tests;
actual provider availability, explicit bounds and spend recording are engineering
preconditions. No real Telegram messages have been sent by this lead.

## Separate native full-baseline PostgreSQL allocation

Update: root explicitly authorized the reserved setup. Cluster creation and native
preflight are complete; setup ownership was released to `/root/baseline_09_runner`.
Actual authority is `docs/qa/native-baseline-allocation-2026-10-01.md`.
The following original reservation/check/creation language describes planning,
not a current approval wait. Engineer/lead now pauses infrastructure writes while
the runner owns tests on immutable 0070/schema 089; no broad test was run by setup.

Read-only inventory at 2026-10-01 02:03 Europe/Amsterdam found active loopback
listeners 58403/58404/58413/58414 and developer PostgreSQL 55432. No listener on
proposed 58441 and no baseline project/container/volume/network below was present.
Existing ce427 resources run; older stopped stand resources remain preserved.
Repeat collision checks immediately before approved creation. Reservation only:
no cluster has been created, started or deleted by this plan.

| Resource | Exact reservation |
| --- | --- |
| Compose project | `synthetic-qa-zns-native-baseline-20261001` |
| Container | `synthetic-qa-zns-native-baseline-20261001-postgres-1` |
| Data volume | `synthetic-qa-zns-native-baseline-20261001-pgdata` |
| Dedicated bridge network | `synthetic-qa-zns-native-baseline-20261001-host` |
| Host endpoint | `127.0.0.1:58441` → PostgreSQL5432 |
| Initial disposable database | `synthetic_qa_zns_native_baseline` |
| Cached image | `postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24` |

This cached PostgreSQL17-alpine image is the actual native developer 55432 image.
No pgvector image was in local inventory. This matches the current native
database baseline, not optional-extension proof. Missing-extension failures or
skips remain explicit; do not silently substitute images or claim parity.

Use a private synthetic credential only at the runtime boundary. Cluster-local
test owner needs CREATEDB and required migration/test role privileges. The harness
creates randomized per-test databases and drops only its own databases
(`platform/README.md`, Tests; importer also uses `TEST_DATABASE_URL`). No copying
developer 55432 databases or production data. Publish only verified loopback 58441
on this supporting cluster's dedicated bridge. No bot/fake/app/provider needed;
do not add a network to existing stands for host access.

Responsibilities and bounded run:

1. Root assigns immutable source epoch and a baseline runner/evidence path separate
   from capability developers. Current 6812 can be an intermediate regression
   baseline; final C/menu 090 still needs resulting-source proof.
2. Engineer is sole infrastructure preparation writer: exact digest/resource
   labels, unique volume/network/credential, readiness/version/extension readback,
   verified native 58441 connection and disposable CREATEDB/drop preflight.
   This plan does not authorize silent creation/start/deletion.
3. Engineer releases setup; runner becomes sole test/data writer. No rebuild,
   restart or reseed during the run. Developer55432 stays available. Baseline
   tests cannot use FQA databases or stands.
4. Run actual native full Go package baseline and the separate importer module
   on 58441 via `TEST_DATABASE_URL`. Use assigned repository-local caches and
   bounded GOMAXPROCS/package parallelism. Capture JSON failures/skips, prerequisites,
   exact source state before/after and raw migration inventory. Serialize modules
   if cluster-global role names or other fixtures overlap; do not weaken gates.
5. This is not Linux race, live UI, Zitadel, real provider or independent Functional
   QA proof. Unsupported live checks remain skipped/blocked. `quality:all` requires
   its own live-stand contract and must not silently target ce427 UI.
6. Root assigns failures to product developers; runner does not edit product code.
   Preserve evidence/cluster until explicit runner release and no reproduction
   depends on it. Cleanup checks exact labels/consumers and never touches 55432,
   ce427 or other projects.

## Final combined allocation authority

Flows/recovery/import remain isolated. Engineer receives final full reviewed Git
ID, actual last migration and raw inventory; lead verifies actual source/image/
config/database handles before freeze. No new build delta or preserved-data reuse
is assumed without assignment.

E reservation: `synthetic-qa-zns-fqa-import`, database
`synthetic_qa_zns_fqa_import`, nominal loopback 58421, marker
`qa.e-import-removal.20261001.synthetic-only`. Reservation is not a verified
endpoint or frozen-schema authority. Preflight actual 58421 connectivity before
`endpoint_verified`; Linux CLI on an owned network needs its reviewed harness
contract instead. Existing internal-only PG host-port assumptions are not proof.

Required E record: reviewed commit, actual last migration,
`status=frozen reviewed_schema/input_reviewed`; writer `e_rehearsal`, roles
`[zns_app,zns_meter]`, verified `managed_stopped`; actual host/port and
`endpoint_verified`; prerequisites Compose/images-env paths and SHA; actual
CLI `repository@sha256`, config volume; `inputs_sha`, source-inventory SHA,
raw-migration-inventory SHA, probe-inventory SHA and probes-manifest SHA.
Schema/source/CLI bytes use the same final export. Earlier 089 CLI preparation is
not authority for final 090 apply/removal.

Exact real Telegram account ID is now a root-owned unresolved PROGRESS question.
Existing bot 6087685431/interaction permission remains recorded; no message until
the final account/recipient contract and main synthetic prerequisites hold.

## Genuine C fixture allocation correction

Actual reviewed pack declares exact stand
`synthetic-qa-zns-registration-fixture` and owning database
`synthetic_qa_zns_registration_fixture`. Existing flows/recovery templates and
databases have different names and cannot install the pack unchanged. Lead checked
the actual pack contract: init takes its opening instant; read and bounded
revoke actions cannot change clocks. No general time-advance control is implied.

Root approved a new isolated full managed stand using the exact pack names;
do not broaden the fixed allowlist or rename/reseed the preserved ce427 resources.
Plan allocation: namespace `synthetic-qa-zns-registration-fixture`, separate
`-prerequisites`, `-runtime`, `-owner` Compose projects; separate `-pgdata`,
`-config`, `-state` volumes and `-front`/internal networks; proposed
loopback 58431 PostgreSQL / 58432 app / 58433 UI / 58434 control. This is future allocation only;
repeat exact resource/port checks before approved preparation. Verify actual
operator endpoints rather than assuming internal-network hostports work.

The fixture requires normal product setup first and the three original synthetic
identities. Do not fabricate a fourth identity or a payment administrator for
event B. Event B currently supports catalogue/negative ACL checks only; ordinary
event-B completion remains a payment-admin prerequisite, not passed parity.
Document actual event A/B constraints, roles and public sanitized evidence in
the next handoff. Registration unfinished ten-minute ranks, invitation expiry
and runtime recovery remain separate requirements/control contracts.

Final build authority still requires exact reviewed combined source, relevant
menu 090/E guards and fresh actual binary/image/raw migration bindings. New C
resource authorization is not permission to start an unreviewed image or modify
the isolated native baseline while its runner is active.

## C registration clock allocation and reader contract

Planning handoff on 2026-10-01; no resources or clock state created. Root approved
the core reader separately. Operator adapter paths below are proposals awaiting
root assignment and fresh Code QA. No fake-provider business clock is permitted.

Allocation correction: C uses installation `010400000204`; E retains its existing
`010400000203` reservation. Read-only current allocation files and all Docker
container installation labels were checked on 2026-10-01: 204 has no existing
reservation or container; flows201/recovery202 remain present and import203 is
reserved. Shared-host coordinator inventory requires distinct installation labels
across projects. Core developer was notified before adapter/code freeze. No
started resource was affected. Repeat this check immediately before preparation.

| Binding | Exact value |
| --- | --- |
| Stand | `synthetic-qa-zns-registration-fixture` |
| Database | `synthetic_qa_zns_registration_fixture` |
| Installation | `010400000204` |
| Case | `c-registration-clock-20261001-v1` |
| Runtime database address | `postgres:5432/synthetic_qa_zns_registration_fixture` |
| App database role | `zns_app` |
| Persisted marker in existing `public.zns_sandbox_fixtures` | `registration-clock:010400000204:c-registration-clock-20261001-v1` |
| Persistent clock volume | `synthetic-qa-zns-registration-fixture-clock` |
| App read-only clock mount | `/run/registration-clock` |
| Exact state file | `/run/registration-clock/state.json` |
| Proposed config source | `docs/sandbox/fqa-stands/registration/runtime.yaml` |
| Reserved ports | PostgreSQL58431 / app58432 / UI58433 / provider control58434 |

Projects are the stand prefix plus `-prerequisites`, `-runtime`, `-owner`;
separate volumes are `-pgdata`, `-config`, `-state`, `-clock`; networks are the
exact stand prefix (internal runtime) and `-front` (isolated UI/provider ingress).
The internal service `postgres` is the actual runtime target. Native loopback
58431 remains a reservation until transport is demonstrated; operator Docker
exec on this exact owned PostgreSQL container is a separate supported transport.
No alias, `host.docker.internal`, `127.0.0.1:55432`, alternate port, or same-named
developer database is a clock target. The old developer database is never reused.

The strict JSON reader contract has exactly eight fields: `version:1`,
`installation`, `case`, `stand`, `database_address`, `anchor`, `current`,
`revision`. Identities/address match the table. UTC instants have exact microsecond
precision. The reviewed case setup manifest specifies the immutable anchor and
its relationship to domain opening times; initial current equals anchor and
revision equals 1. An advance supplies expected revision and a strictly later
target at most anchor plus 60 hours, inclusive; successful state advances revision
by one. Every read rejects backward time, lower revisions, and changed state at
the same revision. Unknown fields, invalid precision and mismatched bindings fail
visibly. The fixture's existing `registration-fqa-v1` marker remains distinct.

Launcher additionally binds `REGISTRATION_CLOCK_ANCHOR` to that exact reviewed
UTC RFC3339 microsecond instant. The file anchor equals this setting on every
read, including cold start after replacement. Operator cannot change it in the
current case. The reader hashes raw state bytes to detect changed bytes at the
same revision. Adapter/templates consume the exact settings below; no invented
aliases are authorized.

Core developer confirmed the five settings:
`REGISTRATION_CLOCK_FILE=/run/registration-clock/state.json`,
`REGISTRATION_CLOCK_INSTALLATION=010400000204`,
`REGISTRATION_CLOCK_CASE=c-registration-clock-20261001-v1`,
`REGISTRATION_CLOCK_DATABASE_ADDRESS=postgres:5432/synthetic_qa_zns_registration_fixture`,
and `REGISTRATION_CLOCK_ANCHOR=<reviewed immutable UTC RFC3339 microsecond instant ending Z>`.
All absent preserves the ordinary production clock. Partial/mismatched app
configuration rejects; any present rejects API/bot entry points. No date value
is assigned until the reviewed setup manifest is frozen.

Activation requires sandbox/synthetic app mode, exact configured and actual DB
address, current database owner, original three fixture identities, and the exact
durable clock marker. Name matching alone is insufficient. Unsupported API/bot
entry points reject clock activation visibly. No fourth identity or event-B
payment administrator is added; event B retains catalogue/negative ACL scope.

State is a regular non-symlink file, mode0600 in a private0700 directory, owned by
the actual approved app runtime UID. Preparation must inspect that UID and prove
permissions; no assumed UID or world-readable fallback. App mounts read-only.
Only the assigned operator adapter has the writable mount. Fake, model, ordinary
user, evaluator, media/sticker helpers and coordinator have no clock control or
writable clock mount. The persistent volume survives managed app replacement;
case reset/anchor replacement requires explicit QA release and a new isolated
case preparation window.

Operator writes require an exclusive writer and persistent compare-and-swap:
validate the previous complete state and expected revision, write a private temp
file on the same volume, sync it, atomically rename and sync the directory. Crash
or race must leave an old or new complete valid state, never a partial file or
silently lost advance. A root-approved adapter must demonstrate this on the actual
Linux volume and then release preparation ownership. Reader alone is not proof
of a working time control or restart recovery.

Proposed engineer implementation ownership, requiring a separate root assignment:
`platform/cmd/registrationclockctl/main.go`,
`platform/internal/sandbox/registration_clock_operator.go`, and
`platform/internal/sandbox/registration_clock_operator_internal_test.go`.
The operator owns bounded initial-marker installation/readback and atomic CAS
init/advance/read commands; it must use the already agreed core state types rather
than introduce a second clock. No edits to `fake.go` or core reader paths.
Stand preparation ownership would separately cover
`docs/sandbox/fqa-stands/registration/{prerequisites.compose.yaml,runtime.compose.yaml,owner.compose.yaml,replacement.json,runtime.yaml,fake.runtime.yaml,inventory-role.sql,product-roles.sql,clock-control.md}`
and its explicit allocation entry. These paths are proposals, not an edit grant.

Private operator custody record is reserved at
`qa.local/fqa-registration-20261001/private-clock-owner.json`, ignored and
owner-only. Its path/hash, actual UID and volume/container handles go in the lead
receipt; credentials and any provider control key remain private and separate
from clock state. No public HTTP clock mutation endpoint is planned. Source-blind
FQA receives only sanitized case identity, supported operator requests/readback,
bounded time semantics, observed revision/time receipts and explicit readiness
limitations. Lead coordinates operator actions with the active sole scenario
writer; neither developer nor engineer mutates a frozen stand independently.

Before freeze, require final reviewed exact Git/raw migration export, approved
operator and reader, actual digest-addressed images, genuine fixture/domain
setup, DB/address/marker/identity proof, permission and unsupported-entry-point
negative checks, atomic advance/readback, and persistence across managed replacement.
Port/resource collision inventory repeats immediately before creation. Existing
ce427 stands and baseline58441 remain preserved under their current ownership.

## Nine-template C preparation refinement

Read-only refinement requested by root after the five operator source paths were
assigned. The nine template paths remain unapproved for edits. Registration CLI
below reflects the current genuine product command; operator CLI syntax reflects
the developing candidate, requiring immutable review before execution. Baseline10
has sole58441 ownership; nothing here authorizes role/DB/resource writes there.

Current engineer source ownership supersedes the earlier three-file proposal:
`platform/cmd/registrationclockctl/main.go` and
`platform/internal/sandbox/registration_clock_operator.go`,
`registration_clock_operator_linux.go`,
`registration_clock_operator_unsupported.go`,
`registration_clock_operator_internal_test.go`. This report grants no expansion.

| Proposed registration file | Exact preparation delta |
| --- | --- |
| `prerequisites.compose.yaml` | New204 namespace/DB/resources; ordered PostgreSQL bootstrap, migrate/seed, ordinary product fixture, registration init, clock init/readback, then fake. Distinct environment blocks; no clock variables on ordinary zns fixture/migrate/fake commands. Tools use the owning zns_app DSN, not the old flows postgres DSN. Operator is a bounded one-shot tool, never a seventh managed runtime component. |
| `product-roles.sql` | Fresh-cluster-only zns_app/zns_meter role and schema ownership setup; dedicated database owned by zns_app. No copied old-flow literal credentials. Private runtime credential injection through a reviewed bootstrap mechanism; tracked file contains no secrets. Product migration and fixture tables are created by zns_app, including public.zns_sandbox_fixtures. |
| `inventory-role.sql` | Exact new DB grant to read-only zns_inventory, pg_read_all_stats, read-only defaults and managed-role connection-check settings scoped to this database. No write access to clock/fixture state. |
| `runtime.compose.yaml` | Exact204 managed labels; original six components; app uses zns_app/postgres5432 and five clock settings. App alone mounts clock volume read-only. Helpers receive no clock settings/mount. Bind actual runtime UID, image digests and config/state volumes. |
| `owner.compose.yaml` | New204 coordinator and isolated state/config; preserves existing six-role replacement semantics. Coordinator has no writable clock mount. Operator custody is separate from coordinator/admission ownership. |
| `replacement.json` | Installation204, new runtime Compose project/files, runtime_database_host postgres, managed roles zns_app/zns_meter and new private state directory. Never target import203 or old flows/recovery. |
| `runtime.yaml` | Sandbox/synthetic app, exact C UI/provider addresses and fixture-model endpoint; reviewed case opening and anchor through explicit setup settings, not wall-clock inference. No control grant through model configuration. |
| `fake.runtime.yaml` | New UI58433/provider-control58434, own provider journal/case and private key; actual ACK readback. No business clock or registration substitution. Ordinary user/model cannot mutate clock state. |
| `clock-control.md` | Public sanitized operator requests/readback and private preparation checklist; five fixed settings, atomic CAS/time bounds, permissions, durable marker, restart and unsupported-entry-point evidence. No secret values or implementation findings in reviewer handoff. |

The engineer must select and obtain review of the explicit private bootstrap
mechanism before templates are approved. Docker-entrypoint SQL does not substitute
environment passwords automatically. Do not start a template that assumes it
does, copy old credentials, or make zns_app a superuser through POSTGRES_USER.
The bootstrap administrative connection creates only the fresh allocated roles,
database ownership and required initial schemas. It closes before setup tools.
Prefer all migrations/seed/product fixtures as the actual database-owning zns_app
so their tables/sequences have correct ownership. If a reviewed migration needs
an administrative prerequisite, enumerate and isolate that action first; never
fall back silently to creating all tables as postgres. No database-owner bypass
or weakened factory check is accepted.

Genuine command sequence, using private config/DSN and only this stand network:

1. Current digest-addressed `zns migrate` (sandbox performs migration then seed),
   with all REGISTRATION_FIXTURE_* and REGISTRATION_CLOCK_* settings absent.
2. Same current `zns product-fixture`, with those settings absent, as zns_app.
   This materializes product-v1/product-passport-v1 using Alice101/Bob202/Visitor303.
3. Same `zns product-fixture`, now REGISTRATION_FIXTURE_ACTION=init,
   REGISTRATION_FIXTURE_STAND=synthetic-qa-zns-registration-fixture,
   REGISTRATION_FIXTURE_OPENS_AT=<reviewed setup opening RFC3339 instant>.
   Clock settings remain absent. Init requires the fresh genuine product state;
   no prior booking/intents or altered original role counts are permitted.
4. Operator `/usr/local/bin/registrationclockctl -action init`, with the five
   clock settings and owning zns_app DSN. It owns state publication and exact DB
   clock-marker insertion. Verify complete state and marker after uncertain init
   rather than deleting/resetting state. Then `-action read` with identical settings.
5. Registration read uses `zns product-fixture` with action=read and exact stand;
   opening is absent. Bounded live role changes use only revoke-payment-a or
   revoke-booking-admin, opening absent, under reviewer-coordinated ownership.
   No manually fabricated state or unsupported restore/payment-B action.
6. Subsequent operator advance is `-action advance -expected-revision <observed>
   -target <strictly later UTC microsecond instant>`; record result plus observed
   product UI/domain readback. Never treat operator JSON as a passed business case.

Before managed start, owner readback records current_user, current_database,
pg_get_userbyid(datdba), the exact configured postgres5432 address, and original
three identity pairs. Require current_user=datdba=zns_app. Read fixture table
owner/ACL explicitly using pg_class/pg_namespace and has_table_privilege for
SELECT/INSERT; database ownership alone does not grant access to a postgres-owned
table. Verify genuine product/registration/clock markers and sanitized fixture
read. Table/sequence ownership and runtime access must be demonstrated, not
inferred from default grants on core/bot/interaction. Public fixture markers are
created and used as zns_app; no broad PUBLIC grant or role impersonation fallback.

Build registrationclockctl from the same final reviewed raw Git export as app,
fake and current source inventory, record input and executable hashes, install
at `/usr/local/bin/registrationclockctl` in the approved actual operator image,
then inspect its bytes through the resolvable repository@sha256 binding. ce427
artifacts do not contain this CLI. Do not substitute host executable/current
worktree code for frozen image bytes. The CLI image may be a separately bounded
operator image or share the current application image only after actual installed
binary proof. It remains outside the six-component managed runtime inventory.

Inspect approved app image's actual Linux UID and use that same numeric UID for
the transient operator process and clock directory/file ownership. Operator gets
only clock RW + config RO on the allocated network; app gets clock RO + config
RO. Parent0700/file0600 and current euid checks must pass in both real containers.
No chmod0777, supplementary-reader workaround or root app fallback. Atomic state,
CAS lock and directory synchronization live on the persistent clock volume,
outside launch-generation state. Prove equal case/anchor and later revision after
managed replacement; no anchor reset, volume recreation or provider-fixture
reinstallation during frozen QA.

The original40 scenarios remain individual acceptance rows. This preparation
can enable genuine manual registration portions of F01/F02, C role portions of
F06, rank/time portions of F09-F11 and R15/R16, and a bounded subset of F12/F13
once execution proves their controls. ACK can enable the missing receipt portion
of R01/F01. None is a full-row PASS from preparation. F03/F04 language-fallback,
F05-F07 private knowledge/consent/history, F08 model delay/interruption, F10 ingress
burst/reversed replay and F12 last-place concurrency still need their exact public
setup/control proof; event-B completion remains blocked. R02-R14 durable delivery,
crash/session/SQL/fault/lane controls and R17/R18 diagnostics/resources are separate
readiness contracts. All nine I rows remain in the isolated E allocation. Retain
the required nonreplayable consumed-model-before-save/provider-reboot case.

Final freeze needs integrated reviewed C core/operator, menu090 and applicable
job/source changes, exact raw migration bytes, actual image/binary/config/DB/UID
bindings, and new source-blind independent FQA ownership. Nine-file approval,
build authority and resource preparation are distinct from this planning report.

## Conditional role expansion and current acceptance state

Root approved development of four fixed genuine fixture actions: restore-payment-a
for Bob/event A, grant-payment-b and revoke-payment-b for Bob/event B, and
restore-booking-admin for Visitor. Same original three identities, fixed explicit
actions, idempotence, owning DB/identity guards and locks; no generic grant API,
clock/schema changes or init-replay restoration of grants. Candidate672 is under
fresh QA255; E10 received QA256 FAIL for a localhost endpoint P2 issue and its
author is fixing it. These are pending source acceptance, not available
stand controls. Until gates/review/integration and actual final-image proof,
current5057 and ce427 retain the earlier event-B negative-only limitation.

Once accepted, registration template contract may expose only those exact action
names supplied by the reviewed parser, with opening absent and current role
readback. This enables genuine B payment cases and reversible role trials without
a fourth identity; it does not itself pass the original40 scenarios. Engineer
separately owns nine registration templates in a new worktree, without resource
authority. Lead owns this plan, not those templates or product source.

Focused10 terminal9PASS/1FAIL/0SKIP/0unfinished is narrow reproduction evidence;
full09 remains failed/incomplete, including its45-minute integration timeout.
Neither role source review nor focused10 replaces final broad regression/FQA.

## Final broad native coverage schedule

Recommendation: two isolated PostgreSQL clusters, two source-frozen runner
worktrees/caches, one writer per cluster. The integration package dominated run09
with2701.086seconds before timeout; longest other package was internal/bot149.64s.
Current read-only source scan found399 integration test files and1048 top-level
Test/Fuzz/Example candidates. This is planning evidence only; the final reviewed
epoch's actual Go test discovery determines the authoritative union. No tests or
test discovery executable was run during planning.

Retain the original platform scope exactly: ./cmd/... ./internal/...
./integration/... ./identityprovision/... ./deploy/... and the separate
tools/migrate module ./.... No -short, assertion/time-limit edits, new exclusions,
environment tricks to cause skips, or replacement by focused examples. Each
existing test's internal concurrency remains unchanged. Scheduling -p=1 and
-parallel=1 matches run09; GOMAXPROCS2, GOWORKoff, local pinned Go and readonly
module input remain recorded. Fresh final source/raw migration byte inventories
before/after are mandatory; earlier0070/5057 results are historical evidence.

After exact final-source authority and stand preflight, runner captures sorted
package union via Go package discovery and complete top-level runnable
Test/Example/Fuzz discovery for each integration package. Execute discovery only
inside the assigned writer window: package init/TestMain can touch its database.
Store raw discovery, command and environment provenance plus hashes. Check the
AST inventory against discovery; investigate omissions before testing. Benchmarks
are not added to the original go test gate. Any extra integration package in the
final epoch must be included rather than silently assuming the current single
package structure.

For every integration package, sort runnable names by ordinal byte order and
assign index modulo8 into eight immutable disjoint manifests I0-I7. Current scan
would place131 candidates each; final count may differ. Each command uses a
properly escaped anchored ^(name1|name2|...)$ -run expression from its manifest,
-count=1 -json -timeout=45m -p=1 -parallel=1. Selecting a parent selects all its
subtests/fuzz seed corpus; do not shard subtests or omit examples/fuzz seeds.
Pass regex as a direct argument, never through interpolated shell code. If command
length exceeds native limits, deterministic smaller child manifests replace that
shard and their union must equal it; coverage is not dropped. Forty-five minutes
is retained per integration shard, never shortened to hide a slow test.

| Runner/cluster | Serial command schedule |
| --- | --- |
| A /58441 | Full unfiltered cmd/internal/identityprovision/deploy package union, then I0,I2,I4,I6 |
| B /proposed58451 | I1,I3,I5,I7, then full unfiltered tools/migrate ./... |

Other platform packages retain45-minute per-package timeout; importer retains15m
as run09. No two commands share a cluster concurrently. In a one-cluster fallback,
run other-platform, I0-I7, importer serially on58441; same exact union and bounds.
Two clusters can roughly halve the integration critical path if balanced, but
no speed estimate or PASS is promised. Eight independent package budgets bound
hangs and permit remaining manifests to complete after a failure; global coverage
is the complete union, not a shorter aggregate deadline. A timeout remains FAIL
plus incomplete coverage; preserve unfinished names and finish the other shards.

Cluster separation is necessary because randomized test databases do not isolate
cluster-wide roles/default role settings and restricted-role migrations. Internal
package tests, integration tests and importer never write the same cluster at
once. No runner targets developer55432, C204, ce427, or E resources. Host listeners,
global process environment, temp paths, file fixtures and CPU/memory contention
remain independent risks: audit final test bindings and use separate processes,
worktrees/caches/temp roots. Tests with fixed host ports or shared local resources
must run in a serialized manifest window across both runners, preserving their
membership and assertions. Concurrent cluster ownership is not permission to
alter a shared role in another cluster or weaken wait assertions for machine load.

Minimal additional proposal (no creation/start authority yet): project
synthetic-qa-zns-native-shard-20261001; container same prefix-postgres-1;
volume same prefix-pgdata; bridge same prefix-host; initial DB
synthetic_qa_zns_native_shard; loopback127.0.0.1:58451->5432. Use the same cached
actual PostgreSQL17 digest as58441, 2CPU/1GiB/256MiBshm, restart=no, no app/fake
or managed coordinator. Read-only current native/Docker inventory found no58451
binding or matching container/volume/network/reservation. Repeat immediately
before authorized setup. One proposed new config path:
docs/sandbox/fqa-stands/native/shard.compose.yaml, plus a separate owner-only ignored
qa.local/native-shard-20261001/private-owner.json and receipts. Assigned engineer
creates only exact resources, verifies native transport/extensions/disposable DB
create/drop, and privately applies the reviewed native first-three-role prerequisite
before release. zns_app is not added without a specific reviewed test requirement.

Raw reconciliation keys are module/package/top-level test and full nested test
name; retain shard ID, process exit, package terminal event and raw JSON SHA.
Require manifest union equals authoritative discovered union with zero missing or
duplicate top-level executions; all expected parents terminal. Count PASS/FAIL/
SKIP and unfinished separately, preserving nested counts and no-test package
events. A failing parent cannot be erased by child passes or a selected rerun.
Existing source/platform-dependent skips remain explicit unmet proof; the plan
adds no skips. Windows/Linux-only or live prerequisites require separately scoped
supplemental execution, never deletion from reconciliation or claimed success.
Keep initial failures and any later reproduction distinct. Root assigns fixes
and fresh affected review, then a newly frozen final complete union as needed.

Before this schedule runs, root supplies final integrated reviewed source
(including C core/operator/role controls, menu090, E guard and applicable job/source),
settled native prerequisites and independent runner assignments. This is broad
native regression only; pinned lint/race/live integrations and source-blind
Functional QA remain their separate gates. No test, bootstrap or infrastructure
mutation was performed for this plan.

## Second native cluster configuration checkpoint

Root authorized preparation of the single Compose file above, not resource
creation/start, role bootstrap, source execution or tests. The config now reserves
the exact stated cluster/port/digest/limits with external own volume/network,
pull=never and restart=no. NATIVE_SHARD_PASSWORD is runtime-only; no credential or
private owner bundle is generated during this config checkpoint. Final immutable
writer/source/role-bootstrap manifest is assigned separately. The authoritative
eight-shard union comes from final actual Go discovery, with source scans only a
cross-check; unfiltered other-platform and complete importer scopes remain intact.

Actual Compose config --quiet returned exit0 with a temporary non-secret parsing
placeholder, subsequently removed. Docker emitted an inaccessible local config
warning; this command did not contact/start the engine and is syntax proof only.
Compose raw SHA256:
`3C86799B628CD21E3B710FD45E10F8D1C7468DE34F868CE476DE21F9B82687BC`.

Future C204 private bootstrap custody: reserve a separate owner-only ignored
qa.local/fqa-registration-20261001/private-bootstrap.json and private rendered
SQL/config files, plus the previously allocated config and clock volumes. Engineer
preparation writes only after reviewed source and exact resource authority. Windows
ACL protects host private files; it is not proof of Linux UID/mode. A reviewed
Linux preparation process on the owned named volume must create clock directory
0700/file0600 under the actual app/operator numeric UID and prove regular-file,
owner and non-symlink checks from both real containers. The private control state
is persistent volume data, never a Windows bind mount assumed to honor chmod.
App remains RO/operator RW; secrets are neither clock JSON nor reviewer receipts.
Database connection role zns_app is separately verified as current database owner;
PostgreSQL container UID is not the app UID or SQL-role identity. No resources,
private secrets/SQL files, clocks or bootstrap writes exist from this checkpoint.

## F08 consumed-model interruption alignment

Root confirmed original F08 requires bounded termination, no unauthorized or
duplicate mutation, a visible recoverable outcome, a working subsequent intent,
and coherent completed/draft state after interruption/replay. It does not require
fabricated identical replay of an unsaved fake response. Actual consumed-before-
save and provider reboot remain required, with no reset/reinsert/winner injection;
consumed/unavailable alone is not F08 or whole-R-row acceptance.

Lead's operator-only proposal qa.local/model-interruption-observer-plan.md binds
source33540321 and validates actual saved owner/update winner, execution-observation
and selected registration domain-receipt/projection metadata queries. No payload
or private body readback, guessed provider-turn correlation or SQL winner writes.
Sampled zero is exact-key absence at that snapshot, not a global no-effect proof.
The public reviewer contract uses opaque case/owner/update/turn tokens and bounded
status/counts, complemented by real UI draft/outcome/subsequent-intent evidence.
It excludes observer SQL/source details. Different domains and broader recovery
obligations remain separate. Proposal only; no DB query, observer implementation,
test or active stand mutation occurred. Root must assign scoped implementation
and runtime observation windows after review; D barrier/journal controls are
separately owned. Payment owns58451 and clock work owns55432; neither is touched.

## Integrated role-control checkpoint

QA255 independently passed the five-path fixed-role candidate67206220. Root
merged it without conflicts as102072a0; focused sandbox/command tests passed.
The four fixed actions are now integrated source, still absent from sealedce427
images. Template preparation and future stand installation do not certify FQA.

## bd150 private-role prerequisite correction

Root integrated runtime14 at bd150d5b1b6f91977d1597686e889543dab22330 after
QA264 PASS. Raw migrations remain unchanged. Source/template acceptance is not
a running C stand. C reader/operator still block final managed preparation;
D10 successor80d0 awaits full QA266 CLI review and qualified F08 observer;
payment7 is under construction after QA265. Its nine PG passes and D focused PG
passes do not confer acceptance. Telegram recipient choice and ce427 stay unchanged.

Inspected TestRegistrationPrivateRoleComposition requires five private variables:
REGISTRATION_ROLE_TEST_ADMIN_URL, REGISTRATION_ROLE_TEST_OWNER_URL,
REGISTRATION_ROLE_TEST_OPERATOR_URL, REGISTRATION_ROLE_TEST_FAKE_URL and
REGISTRATION_ROLE_TEST_INVENTORY_URL. Missing ADMIN causes an explicit test skip.
ADMIN is bootstrap administrator; OWNER is nonprivileged zns_app owning the exact
synthetic_qa_zns_registration_fixture database; OPERATOR is private nonprivileged
zns_registration_operator without memberships; FAKE is private zns_fake;
INVENTORY is read-only zns_inventory with pg_read_all_stats. Only app/meter count
as managed session roles. The test intentionally changes role privileges,
memberships, database/table ownership, identities and markers then restores them.
It must never target native58441/58451, retained fixture data or active C204.

Fresh bootstrap supplies roles/schema ownership only. The test migrates/seeds as
zns_app, installs genuine product/registration fixtures, then applies reviewed
runtime-roles.sql itself. Do not pre-install fixture data. Its relative file
../../../docs/sandbox/fqa-stands/registration/runtime-roles.sql requires the exact
checkout layout; include raw ACL/bootstrap files in final provenance alongside
module/migration inventories. Fake persistence here uses actual PostgreSQL plus
httptest; it is not live browser/provider acceptance and requires no clock volume.

Separate TestRegistrationFixtureRealStateAndRevocation requires private
REGISTRATION_FIXTURE_TEST_DATABASE_URL with exact127.0.0.1 host, fixed DB name and
actual owner. It explicitly DROPs core/bot/interaction/credits schemas and public
migration/fixture tables at start. Do not share its target with the private-role
test, retained fixture state or managed stand. Original package/shard membership
is preserved; prerequisites route each test to its own synthetic allocation.

Concrete proposed next allocations, no resource/template grant:

| Purpose | Proposed project / port / database |
| --- | --- |
| Private-role composition | synthetic-qa-zns-registration-role-test-20261001 /127.0.0.1:58461 /synthetic_qa_zns_registration_fixture |
| Original destructive fixture | synthetic-qa-zns-registration-fixture-test-20261001 /127.0.0.1:58471 /synthetic_qa_zns_registration_fixture |

Each has one pinned PostgreSQL17 container, separate -pgdata/-host and private
bootstrap-secret volume/bundle; no installation204 label/coordinator/clock/app
runtime. Proposed config paths: docs/sandbox/fqa-stands/native/
registration-role-test.compose.yaml and registration-fixture-test.compose.yaml.
Role-test receives exact reviewed private role bootstrap; fixture-test receives
owning nonprivileged zns_app and only its reviewed schema prerequisites. Never
add private fixture roles to native parents. Read-only native/reservation check
found58461 unused;58471 requires full collision/capacity inventory at assignment.
No resource was created. Host bundles use protected Windows ACL; Linux secret
volume uses actual PostgreSQL UID,0700 directory/0400 regular files, proven by
stat/readability. Windows chmod or SQL role identity is not OS-owner evidence.

Runner A owns58441 plus fresh58461 for its unfiltered internal/sandbox package;
enable private-role URL variables only for that package. Original destructive
integration test retains its I0-I7 membership; its runner receives fresh58471
URL only in that manifest's explicit exclusive window. Transfer special-cluster
ownership only after terminal process release. No simultaneous writers or silent
reset after failure; re-execution needs reviewed fresh-state allocation. Two
normal native clusters alone cannot supply these prerequisites safely.

Every final Go-discovered test receives a prerequisite classification before
commands freeze; no silent missing-env skip/exclusion. Preserve unfiltered
nonintegration, complete eight-shard test union and full importer. Supplemental
receipts reconcile actual skipped native names to genuine required stand types:

| Requirement | Qualified execution prerequisite |
| --- | --- |
| Ordinary native PG | TEST_DATABASE_URL58441/58451, reviewed first-three roles; randomized test DBs |
| Private/destructive fixture | Exact five role URLs on58461 and original fixture URL58471, owner/ACL/raw-file proof |
| Linux ownership, RO/RW, lock/reaping/symlink | Same final source in owned Linux runner/volumes; real UID/mount/proc/lock checks |
| Physical replacement | ZNS_REPLACEMENT_DOCKER_TEST=1 plus ZNS_REPLACEMENT_TEST_IMAGE@sha256, ZNS_REPLACEMENT_TEST_NETWORK and ZNS_REPLACEMENT_TEST_PG_HOST on isolated Linux lifecycle stand |
| Native media/sticker | Linux ffmpeg/ffprobe/tgs-render and decoder image provenance; STICKER_NATIVE_TEST=1 |
| Script worker | SCRIPT_TEST_SOCKET on an owned actual IPC service/socket |
| Browser/live UI | MARKDOWN_BROWSER/TIMETABLE_BROWSER/CONTACT_BROWSER with NODE_BINARY; BROWSER_AUTH_STAND_FILE/lifetime and Playwright; SANDBOX_URL to a newly frozen owned stand |
| Identity | ZITADEL_LOCAL_STATE and ZITADEL_PROVISIONING_PROOF_STATE from a synthetic current Zitadel API stand |
| Real model | ZNS_LIVE_CODEX_EXECUTABLE under scoped real-provider access; fixtures do not prove reasoning |

Reinspect exact settings/test requirements at final epoch; no guessed environment
toggles. Missing endpoint/OS/mount/image/provider/human recipient produces BLOCKED
names. Source-declared native skips stay in raw counts and require supplemental
proof before whole coverage; no assertion/deadline changes or added -short.
Qualified F08 sampled saved-result/effect observer and real UI/draft/next-intent
evidence remain separately needed; provider consumed state alone is insufficient.
This correction made no test/DB/bootstrap/resource writes. Root assigns each
future preparation and exclusive runner window.

Root subsequently authorized and lead completed only bare special PG setup:
58461 private-role composition and58471 destructive fixture clusters, separate
exact projects/networks/pgdata and unique private bundles, same cached digest,
1CPU/768MiB/256MiB each. Native ADMIN/OWNER authentication, DBowner zns_app,
nonprivileged role flags and zero product schemas/public tables are actual proof.
See native-baseline-allocation report for all handles/hashes/origin and safe
root-owned runtime invocation. Both released to root healthy/idle; no migrations,
fixtures/runtime ACL/test runs occurred. Original manifests and final discovery
requirements remain unchanged; root assigns all future codegate writers.

## Exact final discovery and ownership algorithm

Final source is not frozen. Checkpoint187319ce is planning input; C40 gates,
new model10 CLI37303 and payment7 review remain dependencies. No current checkpoint
selector is called a final manifest. Metadata listing uses -list only, never test
execution; current TestMain hooks were inspected to route ordinary list mode to
m.Run, not their special offline-helper exec branch. Final runner rechecks init/
TestMain before discovery and retains immutable source/Go/binary/build-tag inputs.

For each module and execution OS, capture actual Go package discovery for the full
original scope. Capture `go test -mod=readonly -list . -run ^$ -p=1 <package>`
stdout/exit for each package separately; zero runnable names is valid only with
successful package discovery/listing. Parse only Go-listed Test/Fuzz/Example names,
not diagnostics or package result text. Packages without tests remain in the
package union. Do not infer names from text scans or invent Example functions
that Go does not list. Save raw discovery and SHA, and hash raw source/migrations/
required docs ACL/bootstrap inputs. Native list metadata is not a test PASS.

Integration assignment is fully deterministic: each package's actual listed
names, deduplicated only after rejecting duplicate discovery records, are sorted
with byte/ordinal comparison (e.g. StringComparer.Ordinal, not culture-sensitive
Sort-Object). Zero-based index modulo8 assigns I0-I7. Prefix identity includes
module/package; the same name in different packages is not collapsed. Stable
selectors are ^(regex-escaped-name|...)$, supplied as direct arguments. Parent
selection retains every nested subtest/example/fuzz seed; benchmarks are outside
the original gate. Empty shard is explicitly recorded, not fabricated execution.
Recompute at final source and store all eight manifests before scheduling.

Exact owner resolution: runner A owns native58441 and I0/I2/I4/I6; runner B owns
native58451 and I1/I3/I5/I7. A runs full unfiltered cmd/internal/identityprovision/
deploy; B runs full unfiltered tools/migrate. All original45m platform per-package/
integration-shard and15m importer bounds remain. Each final selector is annotated
with the dependency rules below; assignment is determined from its generated
manifest, never moved out or excluded to make green results.

| Exact selector/dependency | Owner and reservation |
| --- | --- |
| internal/sandbox TestRegistrationPrivateRoleComposition | A owns bare58461 during unfiltered sandbox package; five private role URLs, raw current runtime-roles.sql/full checkout |
| integration TestRegistrationFixtureRealStateAndRevocation | Whoever owns its generated Ix gets bare58471 after explicit transfer; fixed owner URL and destructive schema/reset scope only on that allocation |
| integration TestRuntimeReplacementPhysicalBarrier | Generated Ix owner requests dedicated reviewed Linux lifecycle image/network/PG host and session inventory; no ce427 or C204 reuse |
| integration TestLiveSandbox | Generated Ix owner requests frozen isolated live UI SANDBOX_URL; no implicit historical endpoint |
| integration TestMarkdownBrowser | Generated Ix owner enables MARKDOWN_BROWSER/NODE_BINARY and existing test's ephemeral fake/Playwright; independent browser process/context |
| integration TestMassageTimetableBrowser | Generated Ix owner enables TIMETABLE_BROWSER/NODE_BINARY with owned ephemeral UI |
| integration TestRegistrationContactBrowser | Generated Ix owner enables CONTACT_BROWSER/NODE_BINARY with owned ephemeral UI |
| integration TestBrowserAuthUIStand | Generated Ix owner reserves BROWSER_AUTH_STAND_FILE/lifetime, ephemeral listener/state file and terminal cleanup window |
| internal/sandbox TestModelConsumptionDurableRebootAndDeniedReinstallation and TestModelConsumptionSQLDeadlineDoesNotConfirmOrRewind, if final Go-listed | A reserves native host8090 exclusively for the unfiltered sandbox command; private parent58441, new case journal, no retained fixture DB/control state |
| New model10/clock listed names | A owns nonintegration package scope; classify every actual final listed name for Linux/volume/provider prerequisites before command freeze, not from pending candidate names |

Native8090 is one global lead reservation, not one per PG cluster. Source-confirmed
current provider gates use it and previously released it; read-only current socket
inspection found no listener. Future final runs still require fresh collision
check and root-assigned reservation. No root/developer provider gate or other
runner may bind it while A's package process is active. Container internal8090
published on58434 is a distinct endpoint, not authority to steal native8090.
Private-role61 and fixture71 remain separate from both parent clusters; no URL
export spills into other packages. Their sole writers transfer only after process
terminal release, preserving failed state. No reset/rebuild of a retained DB.

Linux qualification is a separate runner C assigned by root: same final source,
own cache/temp roots and actual Linux RO app/RW operator mount/UID0700/0600,
replacement lock/proc/child-reaping, decoder/socket and provider stand contracts.
Native/OS discovery keyed by OS/build tags is reconciled explicitly; a Windows
skipped Linux assertion is not a PASS, and a Linux result does not erase a native
failure. C cannot query/reset58461/58471 or use native8090 without an explicit
cross-runner window. Identity/real-model/browser dependencies need their genuine
owned endpoints, not skip toggles. Missing prerequisite names are BLOCKED until
qualified supplemental execution; no silently reduced union.

Before start, generate per-command records: immutable manifest SHA, module/package,
OS/tags, full selector or unfiltered scope, primary PG owner, special cluster owner,
global port/socket/path reservations, image/source/ACL hashes, timeout and private
configuration provenance (keys/hash only). Check set union(I0..I7)=actual integration
Go discovery, pairwise intersections empty, and other-platform/importer package
unions unchanged. Run no command with unresolved ownership or prerequisite.
Afterward reconcile raw JSON by OS/module/package/test, preserve package exits,
nested PASS/FAIL/SKIP, every discovered top-level terminal or unfinished outcome,
and explicit special-gate/supplemental receipts. Failed or missing scope stays
open; focused passes do not overwrite full09 evidence or original40 FQA rows.

### Actual checkpoint list-only discovery

At187319ce, actual native Go -list discovery (test functions not executed) listed
2187 runnable parent names across63 test-bearing platform packages; integration
has1060. Separate importer discovery lists136 names across2 test packages. The
first attempt could not access default Go cache and exited before discovery;
the successful list-only calls used the lead's existing isolated cache with
scoped escalation. No credential/DB/resource access or test execution occurred.
All final inputs require fresh listing after source freeze; these are metadata,
not PASS counts. Source-text1048 estimate is superseded by actual Go1060.

Ordinal UTF8 name-list hashes below serialize each ordered shard as name+LF,
including trailing LF. Current I0-I3 each133; I4-I7 each132, union1060. No manifest
files or final test commands were created during this read-only planning step.

HEAD was187319ce before discovery and57118257f7fb7a28c4695b28e0489d75be85e8b9
afterward. The intervening commit is docs-only: git diff reports no platform or
tools/migrate changes, and their worktree status is clean. Thus the listed module
inputs match both checkpoints; this still does not establish a final source freeze.

| Shard / owner | Current name-list SHA256 | Special prerequisite selectors |
| --- | --- | --- |
| I0 /A | BA9A631E1A8ECD60CEC108EB32684A2D93172F936B0D1660B890EB62B32C0368 | TestLiveSandbox: frozen live stand |
| I1 /B | 3914228AC8B62F9DF2D5BCBC08B707A05ECCD75A3ACECEA8F726DF558EDB7CFD | TestRegistrationContactBrowser: actual browser; TestRuntimeReplacementPhysicalBarrier: isolated Linux lifecycle |
| I2 /A | 861F7FBABA28EF844746F7454E44A589FB2227E8D1A32D053E93930131F6D638 | TestRegistrationFixtureRealStateAndRevocation:58471; TestBrowserAuthUIStand and TestMarkdownBrowser: browser/ephemeral state |
| I3 /B | 43F7C8F392C38686E6B200E6A45E291F7018E80F07C11D6090CB86210FD58B0A | Normal PG plus any final classified prerequisites |
| I4 /A | 19DA95F990F961C73746B5CDE5006E02D4839EFCD6734C4B8C608E884D1CA664 | Normal PG plus any final classified prerequisites |
| I5 /B | 2699B656D8BA77554799CD0ACFFAC8D747F00E30F3467BEC96359FB33C3A040F | Normal PG plus any final classified prerequisites |
| I6 /A | 6A392DCEC5A768332B7D417C79C9B6CE8D0D91E3FA9702CDD51B4ACBA07ED60C | Normal PG plus any final classified prerequisites |
| I7 /B | F9396DB9D6C3D62CDCF1BEEF64C1D7FAC997B245B328207E066A15C6978144C5 | TestMassageTimetableBrowser: actual browser |

Go confirms TestRegistrationPrivateRoleComposition in internal/sandbox: it remains
in A's unfiltered package with58461; no special test is removed. Pending model/C
source will add or change names and their indexes, invalidating these preliminary
hashes/assignments; final all-name manifest is recomputed exactly, not patched
from this table. No named human runner is appointed by labels A/B/C; root assigns
fresh actual agents/exclusive windows when dependencies are ready.
