# AUD-23: restart after database failure

Status: agreed behavior; implementation inspected and runtime admission unit tests passed. Product recovery acceptance remains open.

Daniel selected process exit and supervisor restart for rare transient database failures. Do not add an in-process recovery mode solely for this case.

## Existing mechanisms

- `platform/internal/runtimeapp/admission.go`: the admission monitor returns `ErrLost` when its database probe fails. It does not reacquire ownership in place.
- `platform/internal/runtimeapp/lifecycle.go`: admission loss cancels the owned runtime. Workers must stop and join before ownership cleanup. The context signal is explicitly not a transaction fence.
- `platform/deploy/zns-replacement.service`: `Restart=on-failure` and `RestartSec=10s` already specify supervisor retries. This is source configuration, not proof of a deployed service.
- `platform/cmd/zns/runtime_lifecycle_internal_test.go`: an existing PostgreSQL test terminates the admission connection while an HTTP request is running or draining. It requires request cancellation, cleanup before return, and `ErrLost`. This inspection did not rerun the test.
- `platform/internal/bot/inbox.go`: incoming batches and the received cursor commit together before acknowledgement. Completed updates are removed together with the processed cursor. These mechanisms support replay, but source inspection does not prove recovery of every domain effect.

## Remaining proof

`go test -json -count=1 -parallel=2 ./internal/runtimeapp` passed on 2026-09-29 with `GOMAXPROCS=2` and `GOFLAGS=-p=2`. Evidence: `qa.local/go-resume-20260929/aud23-runtime-unit-1.jsonl` and `.exit`. This verifies the admission lifecycle unit scope, not PostgreSQL interruption or supervisor restart.

The same package also passed `go test -race -json -count=1 -parallel=2 ./internal/runtimeapp` in Linux with CGO enabled (package elapsed 1.034s). Evidence: `qa.local/go-resume-20260929/runtime-linux-race-1.jsonl` and `.exit`. The disposable container used a read-only source mount, two CPUs, and a 2 GiB memory limit. Its tool image derives from pinned Go 1.27 Alpine with GCC, musl-dev and FFmpeg 8.0.1-r1. This remains a package-level race check, not product recovery acceptance.

Run the lifecycle tests on the final source. On an isolated product stand, interrupt PostgreSQL, observe process exit and supervisor restart, restore database availability, and verify recovery of accepted updates without duplicate domain or transport effects. Include a transaction in flight and queued delivery. Check the actual production replacement topology rather than assuming the base Compose restart policy is the effective policy.

The separate writer-fencing requirement remains open. Choosing process restart does not prove that an old transaction cannot commit after ownership loss.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
