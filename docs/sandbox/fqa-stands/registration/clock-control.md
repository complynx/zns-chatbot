# Registration stand preparation and public clock controls

Reviewable templates, not a started or accepted stand. Prepared integration base `33540321`;
the next reviewed composition must include current registration fixtures/role
actions, accepted C clock core, accepted operator, menu 090 and applicable source
changes. Operator `7841e785` and the clock core are separate pending dependencies.
No copied development state file or old image satisfies this requirement.

## Allocation and ownership

Lead owns allocation, freeze, scenario writers and fault windows. Engineer owns
preparation only during an explicit lead window. Preserve ce427 evidence and
baseline on port 58441. No compose up/run, volume/network creation, build, database write
or seed is authorized by this document alone.

- Stand/project prefix: synthetic-qa-zns-registration-fixture.
- Database: synthetic_qa_zns_registration_fixture; runtime host `postgres`, port 5432.
- Installation `010400000204`; import retains 203.
- Case `c-registration-clock-20261001-v1`; marker
  registration-clock:010400000204:c-registration-clock-20261001-v1.
- Projects: -prerequisites, -runtime, -owner; networks: exact prefix and -front.
- Separate persistent volumes: -pgdata, -config, -state, -clock and
  -bootstrap-secrets. Clock survives managed launch replacement.
- Requested loopback ports 58431 (PG), 58432 (app), 58433 (UI), 58434 (provider control).
  Verify actual reachability; internal-network port mappings alone are not proof.
  Owned-network CLI and exact owned PostgreSQL `docker exec` remain the preparation
  transports. Public UI uses fake:58433 and its /miniapp/ proxy.

Do not add a seventh managed component. Clock CLI is a transient operator tool.
Only the coordinator starts/replaces the six managed roles. Only the assigned
operator writes the persistent clock volume; app mounts it read-only. Fake,
helpers and coordinator have no clock mount or clock mutation endpoint.

## Private bootstrap and artifact prerequisites

Freeze one reviewed Git export and raw migration inventory. Compile zns and
registrationclockctl from those exact bytes. Bind each actual binary hash and
installed executable to a resolvable repository@sha256 image; inspect Config.Image,
ImageID and actual binary bytes before launch. Do not use a working-tree binary
or assume the old app image contains registrationclockctl. Helpers can be reused
only with compiled-input/binary/image preservation proof.

Inspect actual app image UID/GID and PostgreSQL image UID/GID. Use the actual
nonroot app UID/GID for app, all product tools and operator. No guessed IDs or
root app fallback. Prepare -clock as that UID, directory mode 0700; initial operator
publication produces state.json mode 0600. Prepare coordinator -state with mode 0700 separately.
Config volume/private files must be readable by their allocated users, not PUBLIC.
Record Linux stat evidence; Windows bind-file chmod is not permission proof.

Lead must allocate the extra -bootstrap-secrets volume before preparation. It
contains postgres_password, app_password, meter_password, inventory_password,
each separately generated private credential. Actual PostgreSQL UID owns the
directory mode 0700 and regular files mode 0400. PostgreSQL mounts it read-only; product,
fake, helpers, operator and coordinator do not mount it. No credential bytes,
private DSNs or environment dumps in Git, terminal output or public handoff.

The fresh-cluster entrypoint sources bootstrap-roles.sh only for an empty pgdata.
It uses the existing deployment mechanism: private files, temporary shell
exports, explicit `psql \getenv` and SQL-quoted variables. One `psql --single-transaction`
executes both role files with ON_ERROR_STOP. The helper unsets exports on failure
and success. It does not print passwords or pass them in process arguments.
It ignores psql startup files and disables command echo. Password statements are
excluded from statement/error logs only for the bootstrap transaction; commit
restores the normal server settings.
Bootstrap creates only allocated NOSUPERUSER/NOCREATEDB/NOCREATEROLE/NOREPLICATION
roles, database/schema ownership and scoped inventory permissions. The local
postgres administrative session closes before migrations or product tools.
No admin-login policy change is made; its credential stays outside product config.
Never set POSTGRES_USER=zns_app, retry partial initialization as acceptance, or
fall back to migrating as postgres. A failed bootstrap requires lead-directed
inspection of the new allocated cluster before continuation.

Private environment files are not tracked. Their exact key allowlists are:

| File          | Private settings                                                                                                                                              |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| app.env       | ZNS_DATABASE__URL for zns_app at postgres port 5432/new DB; ZNS_AUTH__SIGNING_KEY; TELEGRAM_TOKEN; OPENAI_API_KEY; MEDIA_WORKER_SECRET; STICKER_WORKER_SECRET |
| fake.env      | ZNS_DATABASE__URL for the same owning zns_app; matching synthetic TELEGRAM_TOKEN; ZNS_AUTH__SIGNING_KEY; OPENAI_API_KEY; R104_CONTROL_KEY                     |
| media.env     | DATABASE_URL for zns_meter/new DB; MEDIA_WORKER_SECRET; synthetic OPENAI_API_KEY                                                                              |
| sticker.env   | STICKER_WORKER_SECRET                                                                                                                                         |
| inventory.env | ZNS_INVENTORY_DATABASE_URL for read-only zns_inventory/new DB                                                                                                 |

Host preparation selects ZNS_REGISTRATION_SECRET_DIR as its protected staging
directory. Copy required raw environment files into the private config volume at
/private before coordinator start. Owner receives inventory.env only and sets
its nested Compose directory to /config/private; app receives app.env. Runtime
Compose reads those files inside the coordinator, not from a guessed host path.
Use no extra `REGISTRATION_FIXTURE_*`, `REGISTRATION_CLOCK_*` or lifecycle identity
keys in private envfiles. Approved Compose 2.40 supports `env_file` with `format: raw`; values
must be generated and URI-encoded correctly, not pasted into shell commands.

Frozen setup supplies REGISTRATION_CLOCK_ANCHOR and REGISTRATION_FIXTURE_OPENS_AT
with their explicit domain relation. No date is selected by these templates.
Keep both immutable throughout this case. The five clock settings are exactly:

```text
REGISTRATION_CLOCK_FILE=/run/registration-clock/state.json
REGISTRATION_CLOCK_INSTALLATION=010400000204
REGISTRATION_CLOCK_CASE=c-registration-clock-20261001-v1
REGISTRATION_CLOCK_DATABASE_ADDRESS=postgres:5432/synthetic_qa_zns_registration_fixture
REGISTRATION_CLOCK_ANCHOR=<reviewed immutable UTC RFC3339 microsecond instant ending Z>
```

## Preparation commands and proof

After separate root resource/build authority, use the reviewed image manifest and
explicit filepaths. These commands omit secret values; Compose resolves private
envfiles. Run each phase alone, inspect its exit/result before proceeding:

```sh
docker compose -f prerequisites.compose.yaml up -d postgres
docker compose -f prerequisites.compose.yaml run --rm --no-deps migrate
docker compose -f prerequisites.compose.yaml run --rm --no-deps fixtures
docker compose -f prerequisites.compose.yaml run --rm --no-deps registration-init
docker compose -f prerequisites.compose.yaml run --rm --no-deps clock-init
docker compose -f prerequisites.compose.yaml run --rm --no-deps clock-read
docker compose -f prerequisites.compose.yaml run --rm --no-deps registration-read
docker compose -f prerequisites.compose.yaml up -d fake
docker compose -f owner.compose.yaml up -d coordinator
```

Prerequisites must be healthy and the prior one-shot phase successful. Do not
rerun ordinary initialization to restore roles, overwrite fixture markers or
reset clocks after QA begins. Genuine migrate performs migration plus sandbox
seed; product-fixture creates product-v1/product-passport-v1; registration-init
materializes the real dedicated events/roles through product transactions.
All those sessions are the actual owning nonsuperuser zns_app, not `SET ROLE` or
an administrative bypass. Clock init publishes initial state then its marker;
after uncertain completion read state and marker before a bounded retry.

Before any app start, execute sanitized owner checks on the exact allocated
PostgreSQL container using `psql -U zns_app -d synthetic_qa_zns_registration_fixture`:

```sql
SELECT current_user,current_database(),pg_get_userbyid(datdba) AS database_owner
FROM pg_database WHERE datname=current_database();
SELECT rolname,rolsuper,rolcreatedb,rolcreaterole,rolreplication
FROM pg_roles WHERE rolname IN ('zns_app','zns_meter','zns_inventory');
SELECT id,telegram_id FROM core.users ORDER BY id;
SELECT n.nspname,c.relname,pg_get_userbyid(c.relowner) AS table_owner,
 has_table_privilege('zns_app',c.oid,'SELECT') AS app_select,
 has_table_privilege('zns_app',c.oid,'INSERT') AS app_insert
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname='public' AND c.relname='zns_sandbox_fixtures';
SELECT name FROM public.zns_sandbox_fixtures ORDER BY name;
```

Require user=database_owner=zns_app; all role power flags `false`; exactly
Alice (`101`), Bob (`202`) and Visitor (`303`). Require public fixture table owner `zns_app` and both
privileges independently `true`. Verify migration/raw hashes, genuine product,
registration and exact clock markers. Schema ownership or PUBLIC/default grants
alone do not satisfy these checks. No extra user is created for event B.

Inspect actual mount paths, readonly flags, UID/GID and clock directory/file
permissions from app/operator containers. Prove unsupported API/bot clock
activation rejects before DB access, wrong binding/anchor/precision/horizon and
stale revision reject, and no ordinary/fake/helper writable mount exists. Use a
separate allocated negative-case window; do not corrupt an active reviewer's file.

## Public operator request contract

After lead freeze, the active scenario writer requests a read or one bounded
advance from the sole operator. Exact CLI requests on the allocated network:
FQA_CLOCK_REVISION comes from the preceding read and FQA_CLOCK_TARGET is the
lead-allocated UTC microsecond target for that scenario.

```sh
docker compose -f prerequisites.compose.yaml run --rm --no-deps clock-operator -action read
docker compose -f prerequisites.compose.yaml run --rm --no-deps clock-operator -action advance -expected-revision "$FQA_CLOCK_REVISION" -target "$FQA_CLOCK_TARGET"
```

Only operator image receives clock read/write; app stays clock read-only. The state has exactly
version `1`, installation, case, stand, database_address, anchor, current, revision.
Initial current=anchor and revision `1`. Advance requires the observed revision and
strictly later current at most anchor plus 60 hours, inclusive, then revision increments
once. Return actual revision/current and record product UI/domain readback; an
operator JSON receipt is not a passed registration scenario. Error/uncertainty
requires readback, never fabricated success or a clock rollback.

Reviewed role actions at integration `102072a0` are read, revoke-payment-a,
restore-payment-a, grant-payment-b, revoke-payment-b, revoke-booking-admin and
restore-booking-admin. Execute only a separately allocated requested action with
REGISTRATION_FIXTURE_ACTION set through compose `run -e` and opening absent;
registration-read already binds the exact stand. Earlier `5057ddb0` does not contain
the four new grant/restore actions. Their existence enables setup, not FQA PASS.
User/model cannot choose these actions. Publish actual roles and event A/B
starting constraints to the reviewer; do not promise event B completion before
its allocated payment role and genuine path are demonstrated.

Managed replacement keeps the same clock volume/settings/case/anchor. Observe
the persisted later revision after real coordinator replacement with retired
old admission/helper sessions. Do not rebuild during QA. Clock process/file
tests alone do not prove product restart, DB outage, invitation/rank semantics,
ACK client receipt or consumed-model-before-plan-save/provider-reboot recovery.
Those remain separate required cases. No reinserting consumed model fixtures.

Final lead readiness requires actual UI EN/RU, current image/schema/roles/clock
bindings and isolation proof. Fresh source-blind FQA receives public identities,
starting roles/times, supported requests/readback and limitations only. Private
credentials, source history and previous findings stay in engineering custody.
