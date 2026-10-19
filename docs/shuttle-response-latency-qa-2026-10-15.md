# Confirmed final-response timing QA — 2026-10-15

The implementation has passed source review, focused PostgreSQL/OTel behavior
checks, all 256 integration race scenarios and the other Go packages. The final
run used 18 fresh bounded integration containers. Strict lint reported zero
issues. The bounded native checks below passed after an explicit operator recovery
of the manual provider. The original automated attempt remains a failure.

The metric contract is in [response latency](shuttle-response-latency.md). This
is root-task acceptance to confirmed final-response delivery, including user and
service waits. It is not business completion or active processing time.

Architecture and Go consultation preceded implementation. Independent source
reviews found a schema rollback race: a concurrent accepted timing fact could
arrive after the guard read and before the column drop. The Down migration now
locks the task table exclusively before checking facts. A real PostgreSQL test
waits for the competing migration lock, commits the accepted fact and verifies
that rollback refuses to erase it. A fresh review found no actionable findings.

The service checks cover pending versus confirmed delivery, multi-chunk final
responses, intermediate prompts, retries, old rows, control tasks, empty results,
uncertain sends, stale receipt attempts and restart without sample replay. A
synthetic receipt-persistence failure after Telegram acceptance rolls back the
receipt and timing together; it does not produce a successful metric sample.

Earlier failed gates remain failures in the evidence. Several old migration tests
attempted all later Down migrations and correctly hit the new protection.
They now apply exactly the migration under test. A subsequent package run failed
the existing IAM cache-pressure test with an authorization denial; three isolated
repeats and the final complete run passed. The cause of that isolated denial is
unproven. No production authorization check was weakened to obtain a passing run.

Only our disposable `shuttle-live-test` environment is used. A readable custom
PostgreSQL backup, source/image hashes, configuration hashes and data baselines
were captured before migration. The candidate is
`sha256:7c70a34388bd72e603293f9050fe84199f1aa3cdd5d95a6155eadc98478252bb`.
Migration 32 is installed; the agent manifest remains `mvp-31`.

One synthetic Telegram request was sent from the authorized current account in
the private test-bot chat. Restart retained the same root task, Loom run and
accepted timestamp. No completion sample appeared while the run was pending.
The first observation timed out: the manual-provider helper had treated a
context-summary request as a final-model request. Database inspection confirmed
that the supplied QA phrase became the summary, while the actual model step was
still pending. This is a QA harness error, not evidence of a new Loom defect.
The same run also contains two model steps with `platform_paid_unknown` and
eventually paused at `platform_retry_limit` while the manual provider waited.
These records are retained without inferring actual paid-provider cost.

The first operator recovery stopped before calling resume: restarting our Loom
exposed a missing file-worker socket mount and readiness returned 503. The config
bytes were restored, but readiness failure prevented its checkpoint flags from
being updated. The helper was corrected to record restoration before probing.
The existing optional file-worker Compose overlay restored the shared read-only
socket, keeping the same Loom image and leaving the worker running. This was
owned-stand configuration repair; Loom developer stands were not touched.

The guarded recovery then temporarily raised the owned mock-provider attempt
limit from two to three, resumed the same original run and immediately answered
its actual pending model step. The exact original configuration was restored.
The original timeout and failed attempts remain in the local evidence. There
was one Telegram request, one delivered final response and no replacement run.
Our Loom was explicitly restarted and recreated during repair; it was not
unchanged throughout this acceptance.

The stored interval was **789.929517 seconds**, including all manual waiting and
service interruptions. The single process-local OTel sample matched it within
one millisecond. The persisted Telegram receipt had the expected private chat
and a positive message ID; the response was also visible in Telegram Web.
Restarting the bot after completion retained the exact timing fields and complete
outbox snapshot, including its receipt. The new metric process contained no
replayed historical sample, and no additional response was sent.

The previous `04b16e…` executable served a real-IAM owner profile read on schema 32.
The candidate was restored afterward. Exact timing/outbox snapshots, prior task,
inbox and outbox rows, selected business/history/config tables and user identity
rows were preserved. Only the current owner's Telegram metadata profile refresh
was allowed, checked against the actual sender fields. Task hashes account for
the four new nullable columns. The disposable full-suite PostgreSQL exited zero
without OOM and was removed; test runners were removed after their results were
saved.

This establishes receipt/timing persistence and metric semantics after explicit
manual-provider recovery. It does not establish unattended model behavior,
automatic recovery from this provider pause, production readiness or complete
trace/shutdown acceptance.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
