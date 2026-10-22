# Account migration slice QA, 2026-10-22

The missing application-account import is implemented as an explicit operator
command. The earlier history and IAM dry-runs retain their original scope. A new
combined protected-copy gate now verifies application import for906 exact existing
subjects and18517 historical records; its count-only boundary is recorded below.
See [operator contract](shuttle-history-account-import.md).

## Changes and focused verification

- Schema73 adds immutable per-source account import receipts. The bounded batch
  revalidates applied users records against the exact archive and derives numeric
  Telegram owners. It creates/reuses canonical application accounts with exact
  reconciled IAM bindings, without changing existing profiles, preferences or
  Telegram metadata. It creates no tasks, jobs or live business state.
- The operator CLI verifies current application-admin authority and every exact
  active IAM target, then rechecks operator authority before commit. IAM names
  are read only for identity verification and discarded. The database deployment
  guard pins bot, organization and issuer. The existing image now packages the CLI.
- Four focused real-PostgreSQL test roots pass after the final store changes:
  creation/reuse; immutable replay and store reopen; current owner-history/avatar
  access; stranger/revoked denial; existing profile/language preservation; conflict
  and final-authorization rollback; non-user source rejection; no new live work.
  PostgreSQL runs in a fresh Docker container; the Go test process runs on Windows.
- CLI tests pass. They cover exact verified targets, initial/final current admin
  authority, foreign authority rejection, strict plan parsing and private argument
  handling. `go vet ./cmd/shuttle-import` passes. Final strict lint on
  `./cmd/shuttle-import ./internal/db ./integration` reports zero issues.
- The existing deployment-guard test passes its bot, organization and issuer
  mismatch cases on the same disposable PostgreSQL. No broad suite is rerun.
- A fresh independent unified review found one real omission: the CLI initially
  did not check the pinned issuer. The normal deployment guard was added. A fresh
  final reviewer reports no actionable findings. Publication and later CI are
  recorded separately below.

## Target artifact

The ordinary Linux/amd64 release Dockerfile builds all three executables. Local
candidate image is
`sha256:7fc2a06aceb576f5d075fde6feeda7cbc47c886fdb06a2b13c708904c7f6a7ff`.
Its source-input manifest pins1083 files, including the new uncommitted files,
with manifest SHA256
`4b9fb54d2aafb7b1f925564a6d0ebe56bd4ff32c9bfc236956d474be67d83164`.
The image carries that manifest label; it is not falsely labelled as a committed
release SHA. The Dockerfile-specific whitelist excludes private caches/configs.
The packaged CLI runs as10001:10001 and returns its expected usage/exit2 in a
read-only, networkless, resource-limited container. This proves Linux packaging
and executable startup, not Linux database import or deployment acceptance.

The first build with networking disabled failed because the builder lacked its
module cache. Its original log remains. The ordinary build then downloaded and
verified the existing locked module dependencies and succeeded; go.mod/go.sum
were not changed. No image was pushed or installed.

## Preserved failures and cleanup

Initial worker compile/lint failures and the root formatting failures are retained
alongside accepted reruns; no original receipt is rewritten. The disposable PG
was owner/ID-checked, dumped and fully decode-checked, then removed with its own
anonymous volume. The decode helper was also removed. The final809-byte dump
contains the harness-cleaned database: individual test schemas were removed by
the harness, so it is not a populated restore or behavioral backup claim.

Private evidence: `.cache/shuttle-work/parallel-acceptance-20261022/migration/`
(test/lint logs, review and cleanup receipts), `review-input-r2.json`,
`root-lint-final.log`, and `candidate-build/acceptance.json`. The independent
business gate in `business/` remains scoped to its previous exact image; no order
implementation changed in this slice.

## Remaining release gates

The permitted count-only production-copy app-account dry-run now passes on an
isolated clone. Historical Telegram names remain hints, not confirmed app names;
legal names remain separate.
Owner-checked avatar references do not require bulk byte migration. Full model
onboarding, prepared human Telegram login and production switch retain separate
approval/acceptance boundaries. Production data and services were untouched.

## Combined real-IAM runtime and published CI

The packaged Linux CLI in image `7fc2a06a` passed a fresh synthetic PostgreSQL/
IAM4.20 gate: two exact accounts imported, identical replay returned the original
receipts, and both local revocation and IAM deactivation prevented later replay.
Exact canonical bindings and fingerprints of all other application tables were
checked through each phase. No confirmed names, tasks, queue jobs or live business
records were created.

Real owner JWTs exercised the current application `InternalHandler` over actual
loopback HTTP in a separately compiled Linux helper. Owner history and exact
avatar record/archive/image metadata passed; another user's JWT was denied.
This is packaged-CLI plus current-source HTTP metadata evidence. It does not
claim that the candidate bot daemon served those HTTP requests or that Telegram
avatar bytes were fetched.

Two earlier fixture attempts failed before archive storage or account import:
first missing message dates caused a nil archive dereference; then plain timestamp
strings failed the legacy Extended JSON date contract. Both failures and all
existing identities were preserved. The accepted continuation consumed a frozen
five-record archive that had first passed offline inspection, creation and
independent validation. It did not recreate the operator or target IAM users.

Fresh independent controller reviews passed. The owned project was archived with
all-database SQL and an application custom-format dump; the application dump was
fully decoded before the two containers, project network and volume were removed.
This is retained populated synthetic evidence, not a production restore rehearsal.
Private receipts are in `parallel-acceptance-20261022/combined-import/`.

Published source `d659209e` passes all six jobs of
[CI39844699908](https://github.com/complynx/zns-chatbot/actions/runs/39844699908),
including909 integration roots across four race-test shards and complete coverage
verification. CI initially found a missing generated receipt model and an outdated
knowledge-inspection contract expectation; both were fixed after fresh review.

## Protected-copy906-account gate

Daniel explicitly authorized a local private IAM snapshot. The source fixture
remained unchanged. The608161-byte snapshot passed full decode before restoration
into an isolated IAM4.20 project with its canonical issuer and TLS route. Offline
machine-only validation matched906 unique archive owners to906 exact existing IAM
subjects and validated18517 historical records. No raw rows or credentials were
displayed or sent to a model.

The packaged CLI in candidate `bd8cc621` imported all906 accounts. The application
contained907 users including one synthetic operator,18517 history records and no
confirmed profile names. Current operator/target IAM checks, identical-plan replay,
receipt equality after store reopen and fingerprints of all other application
tables passed. This is count-only private-copy import evidence, not906-user HTTP
dialogue or avatar-byte acceptance; the earlier synthetic gate supplies HTTP
authorization evidence.

The initial controller failed before IAM bootstrap because Docker did not publish
the gateway port on an internal-only network. An independently reviewed recovery
connected only the gateway to a separate frontend bridge, preserved PostgreSQL,
IAM and the failed checkpoint, and continued the previously unreached phases.
It did not restore the snapshot again or recreate the906 subjects.

Cleanup retained private application/IAM backups, fully decoded both, and removed
all owned containers, volumes and networks. Source metadata and the failed receipt
remained unchanged. No production write, real Telegram request or paid model call
occurred. Aggregate acceptance and cleanup evidence is retained privately under
`parallel-acceptance-20261022/combined-import/private906/`.

Current source `fe8e9d89` passes all six jobs of
[CI39852066416](https://github.com/complynx/zns-chatbot/actions/runs/39852066416):
checks, four PostgreSQL race-test shards and complete-coverage verification.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
