# Shuttle first executable stage: b130aa09

This report records the initial b130aa09 implementation and its checks.
This stage implements durable agent messaging. It is not acceptance of the full
business MVP and is not a production release.

## Delivered behavior

- Private Telegram intake commits messages and River jobs in one PostgreSQL
  transaction. Duplicate updates reuse the accepted task.
- Workers provision identities, obtain user credentials, start Loom runs and
  reconcile their results using persisted operation IDs and request snapshots.
- Questions, answers and cancellation can progress while an original run waits.
  Conversation ordering survives delayed requests and restart.
- IAM and Loom outages postpone accepted work. Uncertain Telegram delivery is
  recorded and is not resent automatically; following chunks are blocked.
- Agent account and task APIs verify subject ownership. Administrative settings
  and local revocation verify roles and invalidate credential caches.
- English/Russian replies, tolerant Markdown-to-entities rendering, private
  metrics and OpenTelemetry propagation are included.
- YAML plus environment variables configure initialization and secrets. Runtime
  settings are stored in PostgreSQL. Migrations are an explicit release step.

## Verification on 2026-10-12

The final source passed Go 1.27.2 tests on Windows and Linux. Fifteen behavioral
integration scenarios use real isolated PostgreSQL schemas and mock Telegram,
IAM and Loom servers. They cover ordinary messages, duplicate intake, lost Loom
receipts, simultaneous dependency outages with restart, ownership and role
denial, question answers, cancellation, ordered steering, unknown delivery,
failed receipt persistence, multipart blocking, message promotion and a closed
Loom TCP listener that later recovers.

Linux tests passed with the race detector. The configured golangci-lint 2.14
reported zero issues. sqlc compilation and module verification passed. Bounded
Markdown fuzzing completed 22,609 executions without a failure.

The final Linux Docker image built successfully. SIGTERM stopped the application
with accepted work pending in 536 ms, including Docker command overhead; exit
code was zero and OOMKilled was false. This measures ordinary shutdown, not every
possible hung-process failure. The process watchdog and Compose stop limit are
five seconds.

Runtime dependency reports record zero govulncheck findings for Windows and Linux
at the audit date. License notices and covered MPL source are included in the
image, together with Go license and patent notices. These checks do not imply
that future vulnerability databases will remain unchanged.

Each implementation stage passed independent textual review. The loops corrected
receipt classification, control ordering, abandoned delivery reconciliation,
unknown-question cancellation, conversation promotion order, credential-cache
bounds and blank Telegram chunks. Adapters and core each completed five passes; distribution completed one pass. Each final pass reported no actionable findings.
The staged baseline remained empty during the review loops.

## Remaining acceptance gates

Real Zitadel impersonation, audience/role settings and bidirectional zitadeltg
linking need a service contract test. Loom IAM-outage continuity needs its own
credential verification cache; Shuttle caching alone cannot establish it.

Persistent Loom chat binding and history context across completed runs are not
implemented. PostgreSQL already stores accepted Telegram history.

File ingestion is not implemented. The selected contract sends attachment IDs
and metadata to Loom and provides authorized reads through Shuttle. Telegram
source loss must return a visible unavailable-file outcome. Loom keeps only
temporary working copies.

Registrations, pairs, partial-payment accounting, ambassador confirmation,
avatars, event time zones, module availability, business tool manifests and old
data/history migration remain later stages. Massage and food/order workflows
follow those stages. Queue-age, database-pool and token-cache dashboards and
alerts also remain pending.

Use [the run guide](shuttle-run.md) and [architecture](shuttle-architecture.md) for
the current runtime contract. [The specification](shuttle-spec.md) remains the
authority for the complete target scope.

Written by Codex (gpt-6/Codex desktop)
on behalf of Daniel Drizhuk
