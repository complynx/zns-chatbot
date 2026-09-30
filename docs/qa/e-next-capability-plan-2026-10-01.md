# E next capability plan — 1 October 2026

Status: read-only investigation complete. Implementation ownership is pending root approval. Integration base inspected: `e72338e1f1b83b259381a4dc3ea2e8afe42f3d8e`. No database, Docker, runtime, importer or product files were changed.

## Bounded next scope

Complete synthetic shared composition by binding `Options.Delivery` before `appservices.NewServices`. Current `platform/integration/notification_delivery_fixture_test.go` calls the constructor first, then rewires selected service values. Constructor-owned copies in `BotDelivery.Food`, `Registration.Intake.(*derivedmutation.NativeRegistrationResolver).Service.Registration` and `AdminUtilities.Registration` retain their original settings. The constructor already supports the required binding; a product constructor rewrite is unnecessary.

Reuse the unaccepted candidate `qa.local/go-resume-20260929/e-shared-composition-completion-20260930`. Its manifest owns exactly the fixture helper and a new copied-service test. Check its before/final hashes against the developer branch before reuse. The candidate test uses a nil pool and verifies that food reaches request validation rather than failing delivery settings, and that native intake receives valid settings. This checks an observable boundary defect rather than duplicating a constructor assignment.

Recommended branch: `codex/e-shared-composition-20261001`, separate worktree `.worktrees/e-shared-composition-20261001`. Exclusive file ownership:

- `platform/integration/notification_delivery_fixture_test.go`
- `platform/integration/shared_composition_copies_test.go`

Planning report ownership remains this file only until root grants the implementation paths. Root owns shared tracking documents. Stand lead owns new E materialization, images, databases, Docker, resources and FQA setup. No edits to `appservices`, migration SQL, importer or frozen historical evidence are needed for this slice.

## Local gates and handoff

Use branch-local `GOCACHE`, pinned Go tools and formatting. First reproduce the copied-service regression with the old helper and the focused test. Then run the fixed non-DB test without claiming PostgreSQL acceptance. Run affected PostgreSQL groups with a separately assigned local synthetic cluster and the existing per-test database helper: native registration intake/authority, food derived delivery, admin utility refresh, notification delivery runtime and shared queue groups. Run affected pinned lint/format on integration and appservices, plus compile checks. No shared database or Docker mutation is permitted until a concrete test-resource owner grants access.

Before requesting Code QA, update the developer branch against the current integration branch, resolve conflicts locally, rerun affected gates and commit only owned paths. Supply exact base/commit, clean worktree status, commands/results and candidate-to-commit mapping. Fresh Code QA receives requirements and immutable source/diff. Stand lead subsequently includes the reviewed slice in a new frozen E epoch; independent Telegram-like EN/RU Functional QA remains mandatory.

## Reusable full synthetic inputs

`qa.local/go-resume-20260929/e-final-schema-completion-20260930/final/source/manifest.json` declares 12 coverage domains and 23 records in 16 files: users, events, passes, orders, order_capacity, massage, messages, files, bot_storage, knowledge, schedule and configuration. Preserve the full source archive and `decisions.json`, `expected-draft.json`, `history-projections.json`, `source-record-projections.json`, `projections.json` and `probe-spec.json` beside it. These include shared pair-proof ownership, old history and long EN/RU bodies. Seven CLI import domains materialize the business data; knowledge/lineup are immutable permanent configuration resources rather than invented additional import receipts.

`e-materialized-20260930-02` contains genuine CLI verify/stage/plans, resolutions, permanent resource copies, `inputs.json` and importer digest `ad227f85fc2d6a16b254ac694744c63b12299e0a20ab4ea705912bce4d6d80f5`. Its status explicitly has `apply:false`, `reconciliation:false` and runtime acceptance pending. Its ledger expectation is 87 actual migrations through 088. This is historical preparation evidence, not the current 089 or pending 090 epoch. Preserve it unchanged.

Reusable removal harness: `e-importer-removal-20260930/final/rehearse.py`, its runtime probe, role inventories, Compose/config/replacement files and bounded no-CASCADE removal logic. Reusable cross-domain service probe: `e-runtime-coverage-20260930/runtime-coverage.go`, exact projections and credit projection SQL. These require refreshed bindings and actual execution; their presence is not acceptance.

## Final E dependencies

Final E materialization must wait for integration migration090 and its reviewed final source to settle. Stand lead creates a new evidence root and source inventory against that exact commit, derives the migration ledger from actual files/checksums, rebuilds the actual importer and product images, and runs real CLI verify/stage/plan/apply/replay/reconcile. Never transplant old plan digests, create substitute success receipts, or relabel the 088 binary/image as current.

After clean reconciliation, archive temporary receipts and isolated importer/module/executable, remove only assigned temporary resources using the preserved harness, and retain permanent legacy references, identity links, proofs, full history, drafts and resources. Build and run normal runtime without importer dependency or credentials, verify before/after permanent-state fingerprints, then restart and exercise real Telegram-like EN/RU interactions. Service probes supplement the UI gate. Independent Code QA and Functional QA must accept this final epoch; this two-file slice alone cannot close E.

Written by E capability developer (gpt-6/Codex)
on behalf of Daniel Drizhuk
