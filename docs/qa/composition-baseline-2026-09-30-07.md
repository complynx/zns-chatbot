# Composition baseline 7 — 30 September 2026

Developer gate evidence, not independent product acceptance. Do not provide this report or its triage to independent reviewers.

The full native run completed with main exit1 and importer exit0. It recorded 4891 passed, 163 failed and 29 skipped test events, counting parents and subtests separately. There were 65 failed top-level groups, all in the platform integration package. There were no panics or tests without a terminal event.

The manifest contained 2091 files with no path or hash changes. This run includes R57–R68 and precedes application of the nine R70/R72–R76 candidate files. Its results do not validate those later changes. The source freeze was released only after the terminal handle and integrity result were checked.

Platform command: `go test -json -count=1 -timeout=45m -p 2 -parallel 4 ./cmd/... ./internal/... ./integration/... ./identityprovision/... ./deploy/...`. The importer ran separately with a 15-minute timeout. Both used the separate local PostgreSQL test service on port55432; synthetic Functional QA databases were not used.

Evidence under `qa.local/go-resume-20260929/`:

- `run-native-baseline-7.ps1`: exact commands and manifest/event accounting.
- `native-pg-all-7.jsonl` and `native-pg-all-7-migrate.jsonl`: full test events.
- `native-pg-all-7-source.json` and `native-pg-all-7-summary.json`: source binding, exits, counts and integrity result.
- `native-pg-all-7-failures-skips.json` and `native-pg-all-7-failed-top-level.txt`: failed and skipped scope.

Composition differs from baseline6. The change in event counts is not the number of repaired defects or a readiness percentage. No race, browser, real-provider, managed-supervisor, production or full-parity acceptance is claimed. Production remains NO-GO.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
