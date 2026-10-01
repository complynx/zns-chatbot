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
