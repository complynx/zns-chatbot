# Consumer tracing through Loom restart, 2026-10-22

The actual consumer gate passed a graceful Loom restart while a model request
was pending. Zinusya automatically continued the original task/run/session,
retained the consumed steering trace and previous-attempt link, and delivered
one final reply. One profile RPC was observed across the complete exported
scenario. Telegram and provider responses were synthetic; no paid model or
real Telegram authorization was used.

## Runtime and recovery

The owned `iam420-zinusya-v1` fixture reused bot source `74604b7e` / image
`0d4457b6`, schema72 / `mvp-92`, published Loom `b2b8d429` / image `9ddd3175`,
and Zitadel4.20.0 / image `9ea0e9ec`. Its earlier IAM projection repair remains
documented in the [IAM report](shuttle-iam420-compat-qa-2026-10-22.md).
The primary/base IAM and Loom developer's resources were unchanged.

An existing synthetic participant requested a profile read, then supplied
steering while the first model request was pending. The stale script was
cancelled and the current profile script executed once. The next model request
remained unanswered when only the recorded Loom container received SIGTERM.
It exited0 without OOM in0.484 seconds. This records dependency stop time; it
does not qualify a Loom timing SLA. The bot's restart timing is covered by the
[preceding consumer gate](shuttle-consumer-tracing-qa-2026-10-22.md).

The original run was persisted as `paused/shutdown`. The same Loom container
was started once. No new Telegram input, operator Resume, queue repair or
replacement run was supplied. Zinusya recovered the same task/run and delegated
session ID automatically. At recovery readback, all eight companion containers
retained their IDs and start times and were running without OOM.

The interrupted model step became `platform_paid_unknown`. Its one sponsored
unknown-attempt record referred to the original step/run/request and retained
positive reserved exposure in the synthetic ledger. Recovery used a new logical
model request; the old mailbox request remained unanswered. The controlled
provider answered only the recovered request with a final reply. The original
task/run completed and one matching outbox message reached `delivered`.

After workflow completion and shutdown, a separately reviewed readback started
only the same owned PostgreSQL container. SELECTs confirmed that the original
unknown step and sponsored-attempt record were unchanged, the run was completed,
and the interrupted request remained unanswered. PostgreSQL then stopped with
exit0/no OOM. This later readback deliberately changes its start epoch; the
companion-epoch comparison above applies to the Loom-recovery interval.

## Trace and cleanup evidence

The private bounded receiver retained174 spans. The original and steering
admission carriers reference exported spans, and task links connect them to
the actual Loom execution. Origin and applied-input carriers stayed identical
across restart. A `run_execution` span started after Loom's recorded restart
uses the consumed steering trace and links the exported previous-attempt span.
The complete scenario has one observed `module_rpc` span, so the recovered
continuation did not add a second observed profile RPC.

The workflow controller and post-completion readback each passed an independent
review with no actionable findings. Configuration transformation was first
checked against copies of the actual CRLF files. All nine fixture containers
stopped with exit0/no OOM. Both original configuration files were restored
byte-for-byte. The collector stopped with exit0/no OOM and was removed.
No product source change was needed.

Evidence remains under
`.cache/shuttle-work/consumer-loom-restart-20261022-v1/`: `checkpoint.json`,
the bounded OTLP journal, all service logs/inspect records and
`post-completion-readback.json`. Earlier failed consumer-fixture preparation
attempts remain preserved in the preceding report.

## Remaining scope

This covers graceful Loom restart during pending inference after a completed
read tool. It does not prove every crash window, uncertain business writes,
approval trace continuation, telemetry collector failure, primary backend
visibility, autonomous model judgment or production readiness. SIGKILL recovery
has [separate older consumer evidence](shuttle-loom-abrupt-recovery-qa-2026-10-22.md)
without this trace gate. Bot restart and the initial steering/RPC graph also
remain separately recorded acceptance results.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
