# Optional local Codex provider

`agent.Codex{Executable: absolutePath, SyntheticOnly: true}` implements the same
`agent.Model` proposal interface as OpenAI. The operator must explicitly attest
that every request contains synthetic data. The adapter cannot distinguish real
personal data from test data. Never connect this provider to production traffic.

The executable path is local operator configuration, never a request parameter.
The adapter accepts bounded text/JSON and validated PNG/JPEG images from
host-derived immutable attachments. Images use private generated files passed
through CLI image arguments and removed after the process exits. Model output
must never supply filesystem paths or command arguments.
The exact model is fixed to `gpt-6-luna`; failures never select another provider
or model. Existing Codex authentication remains in Codex. No credentials are read,
copied, or logged by the application.

Each request creates a private temporary directory containing only the output
schema, passes the bounded input through stdin, and removes the directory after
the process exits. The CLI ignores user configuration and project instructions,
runs ephemerally in a read-only sandbox with approvals set to `never`, and disables
search, shell, apps, browser, image, memory, agent, plugin, hook and code-mode
features using the exact switches in the successful local probe. This is defense
in depth for local synthetic tests, not an OS security boundary for arbitrary CLI
executables. Configure the trusted installed Codex binary only.

The process has a 60-second deadline; parent cancellation also kills it. Each
output stream has a 2 MiB limit, and pipe cleanup has a one-second wait bound.
No raw output, stderr, prompts, or personal names are logged. The adapter does not
launch a shell. Tool features are disabled; any unexpected execution event causes
the response to fail closed. The existing Core/bot executor remains responsible
for applying proposals and checking identity, version and business constraints.

The parser requires one completed turn with one final agent message, checks
required schema fields, rejects unknown plan fields, and reuses existing plan
validation and resource checks. Unknown event types and failed/incomplete turns
are rejected. A single known startup diagnostic is permitted only before the
turn starts: the exact `Code Mode is unavailable because code-mode host is
disabled...` message observed in the probe. Arbitrary error events are rejected.
Observed in-turn reconnect diagnostics for a WebSocket stream closed before
`response.completed` are allowed only with the exact bounded retry message shape.
They do not replace the required completed turn and validated final proposal; tool
events and other errors still fail closed. Provider deadlines preserve their
context error so event processing can retry without discarding private evidence.
Future CLI event changes may require another explicit compatibility update.

Unit tests substitute the test executable for Codex and exercise actual process
I/O, flags, temporary-directory cleanup, cancellation, oversized output, invalid
plans and unexpected events. They do not perform inference. The original
synthetic live probe is recorded in [codex-model-testing.md](codex-model-testing.md);
it proves availability, not end-to-end acceptance of this adapter.

Official reference: [Codex non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode).

## Run the separate local stand

Requirements: Go, Docker Desktop/Compose, Node, and an installed Codex CLI already
authenticated for `gpt-6-luna`. Run from `platform` in a terminal:

```powershell
$env:SYNTHETIC_ONLY = 'true'
$env:CODEX_EXECUTABLE = 'C:/absolute/path/to/codex.exe'
node scripts/model-codex.mjs
```

Use the actual executable path, not a shell alias. The launcher rejects missing
synthetic opt-in or a relative path. The native bot additionally rejects an
unavailable executable or any binding other than `127.0.0.1` for Codex mode.
If Node is not on PATH, invoke its installed absolute path. On this workstation:

```powershell
& 'C:/Users/ddriz/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe' scripts/model-codex.mjs
```

The launcher builds the native binary, starts only the independent PostgreSQL
project in `compose.codex.yaml`, migrates/seeds it, and starts native API, fake
Telegram and bot processes. Every launch rebuilds from current source. Build,
database readiness, migration and service readiness have bounded waits. It does
not rebuild, stop, reconfigure or share volumes with the default deterministic
Compose stand. It does not run model inference until a synthetic chat message
requires it.

| Component | Address |
| --- | --- |
| Telegram-like UI | `http://127.0.0.1:8093` |
| Core API | `http://127.0.0.1:8094` |
| Native miniapp gateway | `http://127.0.0.1:8095` |
| Separate PostgreSQL | `127.0.0.1:55433` |

Codex runs directly inside the native bot's model adapter. There is no model HTTP
endpoint or container-to-host relay. Authentication remains in the user's Codex
installation. All HTTP listeners and the published database port use loopback.
Sandbox signing keys and database passwords are synthetic fixture credentials;
the application containers receive no Codex authentication files.

Type `stop` followed by Enter, or press Ctrl+C, to stop native services. The
launcher owns each child process handle and closes its stdin pipe to request
graceful cancellation on Windows and Unix. `ZNS_PARENT_STDIN=true` enables this
behavior only for launcher-owned native services. If the launcher disappears,
pipe EOF also cancels those services. After seven seconds, the launcher kills
only any remaining owned process tree (`taskkill /PID ... /T /F` on Windows,
SIGKILL to the launcher's child process group on Unix), then waits up to five more seconds. An
unconfirmed exit is reported as a failure, never as successful cleanup. It never kills
processes by executable name. Child windows are hidden on Windows; child logs
are suppressed so prompts or credentials cannot leak through launcher output.

The independent PostgreSQL container and data persist intentionally. To stop it:

```powershell
docker compose --project-name zns-codex-sandbox -f compose.codex.yaml stop
```

Do not add `-v`: preserving the volume allows restart without losing test history.
API/fake/bot partial-start failures close owned native children; PostgreSQL stays
available for diagnosis and a later restart.

## Observed wiring proof

On 2026-09-25, a rendered Edge/Playwright interaction at port 8093 sent the
synthetic question “What is two plus two? Reply in English.” The native bot using
the real Codex adapter returned “2 + 2 = 4.” All four listening ports were
independently observed on `127.0.0.1`. Startup checks rejected false synthetic
opt-in, non-loopback binding and a relative executable path. Parent-process exit
closed all three native listeners while preserving the isolated database.
The explicit `stop` command exited with status zero and closed all native
listeners. A missing executable after API/fake startup failed without fallback
and also closed those listeners. The independent PostgreSQL container remains
available for the next QA run; its history contains only synthetic test data.

This is a focused wiring proof, not independent Functional QA. It does not prove
profile/media behavior, all locales, touch interaction or reliability under
repeated real-model calls. Later source changes require a fresh launcher build
and the normal acceptance gates.
