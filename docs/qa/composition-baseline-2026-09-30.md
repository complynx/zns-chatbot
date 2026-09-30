# Composition baseline 6 — 30 September 2026

Developer gate evidence, not independent product acceptance. Do not provide this report or its triage to independent reviewers.

The full native run finished with main exit 1 and importer exit 0. It recorded 4851 passed, 183 failed and 29 skipped test events. Parent tests and subtests are counted separately. There were 79 failed top-level groups; only the platform integration package failed. There were no panics or tests without a terminal event.

The before/after manifest contained 2086 files with no path or hash changes. This run includes R54–R55 and predates R57. Its results do not validate subsequent changes. The source freeze was released after the manifest check.

The platform command used `go test -json -count=1 -timeout=45m -p 2 -parallel 4` over `./cmd/... ./internal/... ./integration/... ./identityprovision/... ./deploy/...`. The importer ran separately with a 15-minute timeout. PostgreSQL was the separate local test database on port 55432. Synthetic Functional QA used a different isolated database and unchanged R53 image.

Evidence under `qa.local/go-resume-20260929/`:

- `run-native-baseline-6.ps1`: exact command and manifest/counting procedure.
- `native-pg-all-6.jsonl` and `native-pg-all-6-migrate.jsonl`: complete event logs.
- `native-pg-all-6-source.json`: before manifest, checked against the after state.
- `native-pg-all-6-summary.json`: exits, counts and empty source-change/unfinished lists.
- `native-pg-all-6-failures-skips.json` and `native-pg-all-6-failed-top-level.txt`: remaining assertions and skipped scope.

The difference from baseline 5 is not a count of repaired defects because test composition changed. No race, real-provider, systemd/reboot, production or full-parity acceptance is claimed.
