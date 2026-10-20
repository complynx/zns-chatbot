# Full Linux race verification, 2026-10-20

Status: failed. This report does not supersede the accepted scoped runtime
reports. The installed local primary remains schema64/mvp78 with eleven services
and134 Loom runs. No production service or Loom developer stand was changed.

## Full run

A frozen snapshot ran with Go1.27.2, CGO, GCC15.2, PostgreSQL17 and the race
detector on a dedicated internal Docker network. The test runner had two CPUs,
8 GiB memory and a2 GiB Go memory target; PostgreSQL had two CPUs and1 GiB.
The command was:

```sh
go test -race -timeout 20m -mod=readonly -json -count=1 -p=1 -parallel 4 ./cmd/... ./internal/... ./migrations/... ./integration/...
```

There were956 passing root tests,19 failing roots and one intentional watchdog
helper skip. The integration package reached its20-minute timeout with tests
still pending. The process exited1 without OOM. The captured output contains no
race-detector warning; this is not evidence that the incomplete suite is clean.

Seventeen failures concern avatar scenarios, mostly authentication503 responses
while preparing fixtures. One avatar import instead reports an unavailable draft.
The other failures concern delivery of a healthy notification beyond100 waiting
recipients and restoring an approval keyboard after clarification.

The notification's healthy job was persisted beyond the first page. Its unchanged
focused repeat passed. This proves that pagination reached the healthy recipient;
it does not establish the exact cause of the original delivery deadline failure.
A River attempt counter alone does not establish that the job never ran.

For the keyboard failure, a persisted uncertain delivery blocks the subsequent
keyboard message. Extending the assertion deadline cannot resolve that state.
The original run did not retain enough send/receipt timing to distinguish a
cancelled send from failed receipt persistence. The fixture's2-second task attempt
and3-second lease are substantially shorter than production defaults. Load is a
hypothesis, not an established cause. The avatar503 response likewise does not
distinguish an IAM timeout from a database error.

## Follow-up

A separately reviewed diagnostic repeats the19 unchanged failing roots with
`-parallel 1` on fresh PostgreSQL. All19 expected roots passed in83.776 seconds, with no skips, race warnings or
OOM. The exact root-name set was checked against the original19 failures. This
supports a dependence on broad-run conditions; it does not isolate the cause.
The diagnostic stand was archived, fully decoded and removed, with the primary
preserved. This is a diagnostic subset, not replacement acceptance for the full suite. Next verification must
bound integration batch size and concurrency, retain exact test coverage and
investigate any reproducible failure without weakening safety behavior.

The only test-budget source changes preceding the full run were a5-second
fixture teardown bound and an explicit20-minute CI package timeout. Dedicated
process tests retain the1-second soft drain and5-second hard-exit contracts.
Production code and the installed binary were unchanged by this verification.
Five focused process/pair-assignment cases passed before the full run.

The earlier first attempt failed because its QA image lacked GCC. The second
attempt reached the default10-minute timeout after two fixture stop assertions
and blocked cleanup. Both failures are retained separately.

## Evidence and cleanup

Ignored evidence is in `.cache/shuttle-work/linux-race-full-v3/`:
`tests.jsonl`, `tests.stderr.log`, `failure-audit.json`,
`keyboard-failure-observation-v1.json`, `notification-failure-observation-v1.json`
and `checkpoint.json`. The custom PostgreSQL archive is10,897,486 bytes and was
fully decoded with `pg_restore --file=/dev/null`; this is archive validation,
not a restored-runtime test. Both temporary containers, their data volume and
internal network were removed. Frozen inputs and local-primary data preservation
were checked after cleanup. Earlier attempts remain in `linux-race-full-v1/`
and `linux-race-full-v2/`; diagnostic evidence uses `linux-race-diagnosis-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
