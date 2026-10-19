# Drain diagnostics acceptance — 2026-10-15

The bot now reports the duration and observed outcome of its existing drain
phase. This bounded acceptance does not establish worst-case process termination
within five seconds. See the [field and timing contract](shuttle-shutdown-diagnostics.md).

Architecture and Go consultations preceded implementation. The three concurrent
participants, forced HTTP close, admission gate, drain budget, watchdog and
deferred telemetry/database cleanup retain their existing order and behavior.
Only callback results are now retained for a fixed, redacted summary.

## Behavioral checks and review

Real HTTP tests cover an active request finishing successfully while workers
stop, refusal of new connections after drain, and forced closure when the drain
context is already expired. A deliberately late worker can finish after drain
returns. Other tests check classification of a secret-bearing worker error
without logging its contents, and visibility of an expired budget even with
successful callbacks.

The first Linux race pass completed the command package in 1.032 seconds; the
observability package was cached. Strict lint then found a named return and an
exact floating-point assertion. Those were corrected, and test release barriers
were made safe for early failure cleanup. The final command-package race pass
took 1.036 seconds, and strict lint reported zero issues. Both bounded test
containers exited 0 without OOM and were removed. No new full integration or
GitHub CI pass is claimed.

Fresh independent correctness and adversarial quality reviews found no actionable
findings. Reviews assessed bounded result collection, late senders, deterministic
classification, private error handling and behavioral test cleanup. The native
helper also received fresh read-only reviews.

## Owned healthy stop/start

The owned bot installed image
`sha256:c9ada93b9e6db29274f53adf3b556654b7c67486ef6d8bf0722e684d783233a5`.
The 328-path source freeze, successful isolated compiler run and exact SHA256
match between compiler output and image binary were retained. This is recorded
build provenance, not a second independent reproducible build. The scratch QA
image retained predecessor certificates, avatar executable and license notices;
the full production Dockerfile/CI gate was not rerun.

The stand had no unfinished bot tasks. Only its bot was recreated, then stopped
and restarted as the same container. The native stop recorded:

- Drain: **2.440867 ms** against its configured **1,000 ms** budget.
- Overall and all three participant outcomes: `completed`; one INFO summary.
- Docker stop command: **519.9407 ms**, measured with a monotonic host timer.
- Terminal process state: exit 0, no OOM.

The command measurement includes Docker overhead and differs from drain time.
This one healthy observation is below the one-second target and five-second
ceiling; it is not a general latency or stalled-worker guarantee. No native
fault or forced-kill scenario was injected. Deadline/late-worker behavior is
covered by the source tests above.

Restart was placed in the helper's `finally` block. After the same-container
restart, a real IAM owner profile read succeeded. All 27 selected
business/configuration/audit hashes and four inbox/task/outbox/conversation
fingerprints remained equal. Schema 31, manifest `mvp-30`, configuration file
hashes and source freeze matched. Loom retained its exact container ID and start
time. No Telegram message, task, identity or Loom run was created.

## Evidence and remaining gates

Ignored proof: `.cache/shuttle-live-test/shutdown-qa/checkpoint.json`.
Build, lint, binary provenance and source freeze are under
`.cache/shuttle-work/shutdown-qa/`. Linux race logs and terminal states are under
`.cache/shuttle-work/container-results/zinusya-shutdown*-qa-20261015.*`.
The unstarted image-inspection container and compiler/test containers were
removed; the accepted bot remains running on the disposable stand.

A later Linux [watchdog subprocess check](shuttle-watchdog-qa-2026-10-15.md)
proves exit and disarm using the production watchdog with synthetic blocked
cleanup. This healthy-runtime check did not inject that fault. Whole-bot deferred
cleanup and external hard-kill detection still require separate proof. The existing summary cannot expose
every worker lock-release error, and a killed process may emit no final log.
No Cloud log alert or production change was made. Earlier tracing/outage failures
remain [separate and unchanged](shuttle-tracing-qa-2026-10-15.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
