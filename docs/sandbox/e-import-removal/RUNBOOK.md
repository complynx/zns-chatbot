# Git-bound synthetic E rehearsal

This successor reuses the reviewed full synthetic export and import/removal checks.
It is a local operator tool, outside the disposable importer module. It does not
create a stand, change network topology, start/restart services or accept E.
Keep historical088 packages and evidence unchanged. No production data is used.

## Owners and execution boundaries

The stand lead owns the allocation and exclusive mutation barriers. The engineer
owns Compose, images, networks, credentials and stopped managed components. The
E developer owns the six harness/probe source paths. Root owns review routing,
integration and progress. Do not run database/removal actions before final schema
review and a hash-bound frozen allocation. Current allocation reservations are
not executable approvals.

Offline development may use an explicitly named089 epoch. Final E must name its
reviewed090-or-later full Git commit and actual last migration filename. Export
all runtime/importer bytes from that commit with `git archive`; do not build from
a CRLF checkout and compare only Git blobs afterward. `store.Migrate` hashes RAW
embedded SQL bytes. The harness derives the ledger from the exact exported bytes,
checks it before apply and never normalizes or rewrites an applied ledger. Existing
applied_at/checksums are retained in the permanent before/after fingerprints.

## Offline preparation

Provide an operator-reviewed binding JSON and its SHA256. Required fields:

```json
{
  "commit": "<full-40-character-reviewed-commit>",
  "last_migration": "<actual-last-migration-filename>",
  "database": "synthetic_qa_zns_fqa_import",
  "project": "synthetic-qa-zns-fqa-import",
  "marker": "qa.e-import-removal.20261001.synthetic-only",
  "status": "offline",
  "owner": "fqa_lead"
}
```

Use the preserved `e-final-schema-completion-20260930/final` package with its
reviewed package-hashes.json digest, all12 source domains/23 records, decisions,
full-history expectations and permanent resource bytes. Business expectations
stay fixed; only the migration count derives from the selected export. No new
plan digest or success receipt is invented.

```text
python tools/e-rehearsal/prepare.py --repository <own-worktree> \
  --binding <binding.json> --binding-sha256 <sha256> \
  --package <preserved-final-package> \
  --package-inventory <package-hashes.json> --package-inventory-sha256 <sha256> \
  --evidence <own-worktree>/qa.local/<fresh-evidence-root>
```

The directory must not exist. The tool checks dirty/untracked runtime sources,
the exact Git epoch, archive paths and complete source/file hashes. Its real Go
build uses GOWORK=off, GOTOOLCHAIN=local, GOPROXY=off, -mod=readonly and an evidence
cache. Cached dependencies and the pinned local toolchain are prerequisites; no
dependency install is performed. Preparation invokes the real verify/stage,
seven plans and messages validation. Users plans remain genuine CLI JSONL files.
Generated resolutions retain reviewed decisions and bind the newly emitted plan
hashes. Preparation status explicitly leaves apply/reconciliation/runtime pending.

Failed preparation leaves evidence for diagnosis; use a new directory for a
successor. Do not reuse or clean failed evidence to make a command appear green.

## Offline probe compilation

Create a reviewed probe manifest and pass its digest. It has exactly `removal`
and `coverage` entries, each with `source`, `sha256`, `projection` and
`projection_sha256`, plus `test_source` and `test_sha256` for each adjacent main_test.go. Source files are the two successor main.go files under
tools/e-rehearsal/probes. Removal projection is the preserved probe-spec.json;
coverage projection is the preserved e-runtime-coverage expected.json. Bind
the bytes directly. Do not derive expected values from observed runtime output.

```text
python tools/e-rehearsal/rehearse.py compile-probes \
  --inputs <evidence>/inputs.json --inputs-sha256 <sha256> --evidence <evidence> \
  --probes <probes.json> --probes-sha256 <sha256>
```

This copies both hash-bound probes and their focused pre-connection tests into new command directories inside
the isolated platform module. That is the legitimate context for platform/internal
imports. It builds the normal zns command and both probes, records dependencies
and executable hashes, runs the actual compiled pre-connection tests with private hash-bound inputs, and checks every retained source byte. The probe source inventory includes both main.go and main_test.go files. It has no DB or
Docker action and explicitly records importer_absent:false. It does not prove
runtime acceptance or importer removal. Run pinned Go formatting/lint in this
actual compiled module on the two probe command packages.

## Frozen allocation for database/removal actions

The final allocation retains the binding fields, changes status to `frozen`, and
adds reviewed_schema:true, input_reviewed:true, inputs_sha256, writer:e_rehearsal,
managed_roles:[zns_app,zns_meter], managed_stopped:true, prerequisites_compose and
its SHA256, images_env and its SHA256, cli_image as a local repo@sha256 digest,
config_volume below the allocated project prefix, probe_inventory_sha256 and
probes_sha256, runtime_source_inventory_sha256 and raw_migration_inventory_sha256
matching the prepared source. Source/input/probe and actual image provenance
review must precede these flags.

This bounded version supports only transport:host-loopback with host127.0.0.1 or
localhost, an exact port and endpoint_verified:true. MIGRATE_DATABASE_URL must
match that exact host/port/database and postgres owner role. The engineer must
use a URL without a query, or with only the exact `sslmode=disable` query.
All other URI parameters, fragments, target overrides and service indirection
are rejected. Importer commands receive that validated URL with PG environment
settings removed, so the checked authority is the effective pgx target.
The engineer must prove actual access through its reviewed operator topology; Docker PortBindings
alone is insufficient. A Linux CLI in the owned network needs a separately
reviewed transport successor. No automatic port, network or alternate-DB fallback
exists. The credentials stay in the process environment, never the allocation,
runbook, successful receipt or failure output.

The engineer prepares an empty database with SQL migrations built from the SAME
verified export/raw inventory and the exact synthetic ownership comment. No
ordinary product-fixture service may seed business rows. Mount byte-identical
knowledge/lineup permanent config; do not mount export/stage/receipt archives into
the runtime. Keep all managed zns_app/zns_meter sessions stopped during owner
actions. Preserve PostgreSQL and the complete local image/source provenance.

For each owner action add:

```text
--inputs <evidence>/inputs.json --inputs-sha256 <sha256> --evidence <evidence> \
--allocation <frozen-allocation.json> --allocation-sha256 <sha256>
```

Run `import`, then `remove-receipts`, then `archive-importer`. Import runs actual
seven-domain apply/replay, requires reconciliation and zero new replay mutations,
proved by food's explicit `reused:true` and each other domain's integer
`applied:0`. Missing counters never prove replay success. Before any database
or Docker call, owner actions validate actual source, raw migrations, source
export, stages, plans, resolutions, permanent resources and importer binary.
After retirement the archived binary is checked against the same binding.
Import also runs the five CLI-supported standalone reconciliations. Users/events use their
real apply/replay reconciliation summaries; standalone commands do not exist.
Owner/history/draft linkage, exact counts and every permanent table/sequence are
fingerprinted. Receipt removal archives exactly seven tables and uses one
DROP RESTRICT transaction, without CASCADE. Permanent-state mismatch aborts.
Importer archive requires the marked evidence-local copy, the post-removal
fingerprint and unchanged sources; the live checkout is rejected.

Then run `build-runtime` with the same frozen allocation plus the hash-bound
probe manifest. It requires the package/executable absent, uses a fresh cache,
removes importer/runtime/PG credential variables from its build environment,
rejects importer dependencies, and builds product/probes from the same retained
source. Successful build evidence cannot replace execution evidence.

## Runtime and independent acceptance

The engineer starts the restricted runtime with no importer credentials/mounts.
Resolve the actual imported history ID by the preserved probe-spec source key;
keep the other expected fields unchanged. Add exact allocated database, marker, host, integer port, transport:host-loopback
and role:zns_app to both runtime projection JSON files. The build harness checks
these fields against the frozen allocation before copying or building probes.
Review their generated bindings, and pass
the SHA256 of each exact JSON file to its executable. The removal probe takes
`check|delete|tombstone <projection.json> <sha256>`; coverage takes
`<projection.json> <sha256>`. Both validate the URL and effective pgx host/port/database/role before pool creation.
Use only a postgres/postgresql URL with no query or exactly sslmode=disable.
Reject all PG environment settings, service/hostaddr/query target overrides,
keyword/multihost DSNs, unknown transport and SSL fallbacks. localhost resolves
only to 127.0.0.1 in this supported transport. A copied synthetic database at any
other endpoint is rejected before connection, including delete mode.
Database/comment and absent importer schema checks remain before business calls. The assertions preserve history
hashes/tombstones, drafts, domain projections, proof bytes/ACL and credit semantics.
Deletion is a synthetic scenario and occurs only inside the lead's barrier.

Use capture/compare for explicit before/restart persistence checkpoints. These
owner actions require stopped managed writers. The engineer handles actual managed
replacement/restart and its evidence; the harness never fabricates a restart.
Both independent gates and Telegram-like EN/RU manual/agent/UI checks remain
mandatory. Direct service probes, build receipts, unit tests and skipped scenarios
cannot accept the whole stage.

Written by E capability developer (gpt-6/Codex)
on behalf of Daniel Drizhuk
