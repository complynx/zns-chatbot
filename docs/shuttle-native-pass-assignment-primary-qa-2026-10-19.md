# Native solo assignment: local-primary installation QA, 2026-10-19

Status: accepted on the owned `shuttle-live-test` stand. This follows the
[isolated four-path runtime gate](shuttle-native-pass-assignment-runtime-qa-2026-10-19.md).
It does not establish complete registration/pass/payment parity or production readiness.

## Installed result

- Image: `sha256:17763b12faaa79402960c3c37f8b76fa1662d20a7b19e905fde083a183bb5770`.
- Binary SHA256: `96183fd0f0ccc0bed16f1eb6a6e153285c246c94319418ce51088a56dedb1f26`.
- Schema: 61; manifest: `zinusya/mvp-69`.
- Eleven primary services running; 134 Loom runs, unchanged by this gate.
- Fifteen public Mini App assets match current source bytes. The assignment
  events endpoint rejects a missing session with HTTP 401.

The HTTP check is read-only. It is not a new logged-in form-write or real
Telegram WebView acceptance claim. The isolated runtime gate supplies the
four form/agent assignment paths with controlled provider, real IAM/Loom,
mock Telegram and actual Edge.

## Schema expansion and rollback

The same Telegram poller was stopped before draining and stopping the bot.
Custom-format Shuttle, Loom and Zitadel archives were listed and fully decoded
before migration. Archive sizes were 2,897,693 / 2,752,494 / 481,228 bytes.
This validated the archives; no database restore was performed.

One bounded migration container applied migrations 60 and 61. It exited 0
without OOM. All pre-existing domain rows and sequences were preserved;
the two added tables were empty. The expanded schema and domain snapshot
were then preserved through these normal-start binary legs:

1. New image, `mvp-69`, schema 61.
2. Previous image, `mvp-68`, schema 61.
3. New image again, `mvp-69`, schema 61.

The old normal-start path does not run Goose. No Down migration or old
`-migrate` command was used; compatibility of an old automatic-migration
startup is not claimed. The three bot stops took 0.781 / 0.625 / 0.750 seconds,
all exit 0 without OOM. These were idle graceful stops, not SIGKILL recovery.

Readiness, configuration, companion identities, original domain state and Loom
run count were checked on every leg. The same poller was then restarted and its
actual polling cycle observed. The stopped migration container was removed;
no temporary QA database container or volume remains from this stage.

## Source verification and retained failures

The full local Go suite passed 1,014 root tests with no failures. Six
platform/helper tests were skipped. This was Go 1.27.2,
Windows, `CGO_ENABLED=0`, without the race detector; Linux/race remains separate.
All 269 Mini App/browser behavior tests passed, lint reported no issues, and
isolated sqlc compile/generate reproduced all three generated Go files byte for
byte without writing repository files.

The first full Go run remains a failed record: four outdated test assumptions
were corrected after source investigation and independent review; five tests
failed during fixture migration after two concurrent suites exhausted the
QA PostgreSQL lock table. The repeat used one suite on a fresh bounded database
with sufficient lock capacity. The old QA database's slow startup and observer
timeout were retained; its data archive was validated before retirement. Both
QA database containers and volumes were removed with final exit 0 and no OOM.
These fixture failures are not relabelled as successful first attempts.

The installation controller passed three independent review rounds. Fixes
covered live-CLI uncertainty before ingress compensation, stopped/quiet writers
before migration, and exact migration-container custody before recovery.
The final fresh review found no actionable issues. The Git index remained empty.

## Evidence and next scope

Local ignored evidence lives under
`.cache/shuttle-work/native-assignment-install-v1/`: `checkpoint.json`,
`review-loop-final.json`, `installed-epoch.json`, `http-assets-and-custody.json`,
`migration-cleanup.json`, exact CLI/container records and private database archives.
The full-Go result is `manifest-baseline-v2/pg-full-v2.json`; source checks are
under `native-assignment-session-v1/`. Credentials and archives are not in Git.
The current read-only custody helper is `installed_primary_observer_v18.py`.

Next: native atomic pair assignment and both handoffs, then participant partial
payment/top-up and ambassador review as complete workflows. Keep the remaining
[registration/pass acceptance matrix](shuttle-registration-pass-parity-plan.md)
and [infrastructure priorities](shuttle-infrastructure-priorities.md) open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
