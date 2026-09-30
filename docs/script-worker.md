# Isolated JavaScript worker

The application calls `scriptclient.Client` through a Unix socket. The client
accepts bounded JavaScript function bodies and JSON input; it returns JSON data.
Evaluate has no business tools. Execute exposes only host-supplied authorized
tools over bounded callbacks. There are no user credentials, network or filesystem bindings in
the VM. Code cannot change the worker path or process arguments.

`compose.script.yaml` runs a supervisor with network disabled, a read-only root,
all capabilities dropped, no privilege escalation, 128 MiB memory including swap,
one CPU and 32 PIDs. The only mount is a dedicated 1 MiB tmpfs IPC volume. The
socket is mode 0660 under UID/GID 10002; the parent directory is mode 0770.
Mount this volume read-only into the app and grant supplementary GID 10002
(`group_add: ["10002"]`). The app keeps UID 10001. The test image deliberately
uses UID 10001/GID 10002 to verify cross-UID access. No Docker socket is mounted into either process.

Each request starts a new helper process. The service admits one request at a
time and rejects additional requests as busy. Admission occurs before body reads
to bound concurrent request buffers; the server limits the body read to five
seconds. This is a local trusted-client boundary, not a public HTTP service.

The VM interrupt budget covers execution **and result serialization**, including
proxy traps. The child also has an independent two-second process watchdog.
The parent kills and reaps a stalled child after three seconds or cancellation.
Results are byte-bounded in the parent and validated again by the client. A proxy
trap can run code, but cannot escape these budgets or gain host capabilities.

An OOM can stop the whole worker container. Compose restarts it; callers get a
bounded failure and must decide whether to retry. Evaluate has no external
side effects. Execute may commit host effects; the host needs durable receipts
and must not infer rollback from script failure. The main application and its durable Telegram inbox are separate.

## Verification

- Helper and supervisor process tests: calculation, unavailable host globals,
  infinite-loop failure and successful next request.
- Client contract tests: duplicate/case-alias keys, trailing data, contradictory
  result/error, oversized output and cancellation.
- `TestRealUnixWorker`: real networkless containers, Unix socket, calculations,
  infinite script and serializer-proxy loops, recovery and absent host globals.
- Deployed Docker limits inspected. The real-container test passed in 0.43 s.
- Helper Code QA: no actionable findings. Transport Code QA claims were checked:
  proxy execution is inside the VM/process budgets; admission before reads is
  deliberate bounded resource admission; malformed error payloads remain failures
  and are never returned to the model as successful results.

Run from the repository root:

```powershell
docker --context desktop-linux compose -f compose.script.yaml up -d --build
docker --context desktop-linux build -f platform/Dockerfile.script --target test -t zns-script-test:local platform
docker --context desktop-linux run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --memory 64m --memory-swap 64m --pids-limit 32 -e SCRIPT_TEST_SOCKET=/run/script-ipc/evaluate.sock --mount type=volume,source=zns-script_script-ipc,target=/run/script-ipc,readonly zns-script-test:local '-test.run=^TestRealUnixWorker$' '-test.v'
docker --context desktop-linux compose -f compose.script.yaml stop
```

Agent tool dispatch and end-user Functional QA are not yet accepted.

## Execute transport

Execute uses CONNECT /execute on the same Unix socket and admission slot. The
supervisor relays bounded jrpc2 records to a fresh child. The existing C5 limits
are 60 seconds for the shared execution, 61 seconds for the child watchdog and
62 seconds for transport. Each host operation is bounded by ten seconds and the
remaining shared deadline; an earlier parent deadline wins. Callbacks cannot
renew that deadline. The cumulative 200 ms VM-active elapsed-time budget pauses
during host calls; it includes serialization and scheduling delay, not just CPU
time. Canonical values are in `platform/internal/scriptprotocol/budgets.go`.
These limits supersede the earlier 5/6/7-second Execute limits, but do not close
or change the separate five-second D-001 acceptance check.
The child receives only code, explicit input and caller-visible tool metadata.
See [agent-scripting.md](agent-scripting.md) for discovery, callback validation,
byte budgets and durable mutation requirements. Legacy Evaluate limits and its
data-only contract are unchanged. Fresh Execute Code QA and Functional QA are
required; the earlier verification above applies to Evaluate only.


