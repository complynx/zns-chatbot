# Specialist notification queue metrics QA

The queue metrics query omitted `zinusya_massage_specialist_notice`, although
the worker and metric label allowlist already included it. Due notifications
could therefore be absent from backlog counts and oldest scheduled age.
The query now includes that existing job kind.

## Verification

The real-PostgreSQL behavioral test inserts one due and one future job for each
of `available`, `scheduled` and `retryable`. It verifies the three specialist
notification gauges alongside the existing kinds. Future jobs, unrelated job
kinds, private queues and other states remain excluded.

The original code returned 12 metric rows where the extended fixture required
15. After the fix, all three queue metrics integration scenarios passed with
the Linux race detector. These also cover a blocked collection query and real
River backlog promotion, drain and snooze. The focused task/integration lint
gate reported zero issues. Its first sandboxed invocation could not read the
installed Go standard library; that environmental failure is retained separately.
An independent source review reported no actionable findings.

The fixture used its own PostgreSQL and network, with a 3 GiB test-runner memory
limit and no swap. Neither runner was OOM-killed. The main stand was not upgraded.
This is focused query/exporter acceptance, not a real Telegram delivery test or
full rewrite acceptance.

Local evidence: `.cache/shuttle-work/queue-specialist-metrics-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
