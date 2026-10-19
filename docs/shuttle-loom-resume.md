# Recovering Loom shutdown pauses

Actual Loom preserves a run interrupted by process shutdown as paused with reason
shutdown. Polling alone does not queue it again. Shuttle resumes that existing run
through POST /v1/runs/{id}/resume with the existing delegation and a durable key.
It creates no replacement run, auth session or accepted Telegram task.

The key includes task ID and the observed positive pause version, and is saved
before dispatch. Unknown transport outcomes retry the same key after re-reading
the run. Observing the run progressing clears the pending resume; a later shutdown
uses a new operation key for its new version. A successful receipt is validated
against the original run ID and a positive receipt version. A fresh run read,
not sparse control metadata, drives result/question/approval delivery. An HTTP 409 waits for Loom reconciliation
rather than terminalizing the accepted task or bypassing an unknown inference.

Automatic resume is dispatched only after observing paused/shutdown. Client pause, authorization,
provider retry exhaustion, billing and other reasons do not trigger resume. Local
revocation and token checks still precede calls. Paused or reconciling runs are
represented locally as awaiting_service unless they expose a question or approval.

This handles continuation of an already requested task. It does not authorize
proactive agent work or unknown-effect replay. Loom owns its provider ledger and
reconciliation guard; Shuttle cannot settle unknown model charges automatically.

Behavioral checks cover a dropped receipt before effect, blocked reconciliation,
Shuttle restart with the same resume key, a lost receipt after effect, later
shutdown with a fresh key, one original run/delivery, and other pause reasons.
Full Linux behavioral tests with the race detector and golangci-lint passed.
Two independent textual reviews found no remaining Shuttle defect.

## Concurrent control limitation

Loom resume accepts an empty body and does not atomically check the version or
pause reason observed by Shuttle. A concurrent external controller can change
shutdown to client_pause between the read and resume. Another read cannot close
that race. Strict preservation under competing controllers requires a Loom
expected-version/reason precondition. Until that exists, automatic recovery
assumes Shuttle is the sole run controller. The excluded-reason tests prove
observed reasons, not a concurrency guarantee.

## Real-service acceptance

The first simultaneous Loom/Zitadel outage test exposed polling without resume:
original task f034cf06-2f07-4f06-8498-5bf0014df5fe remained waiting while Loom run
81d5715d-d85b-4597-a167-41a65f4039fe was paused/shutdown. That failure is retained.
The pending provider step was completed before recovery; no unknown inference
was overridden.

Repeat acceptance on image
sha256:90b1a04a540056ec387acf89469f4fac7610c10626814e7d31ff40de05c0441b
resumed that original run, then completed the same task with one delivered outbox
row. Telegram Web showed Сервисы восстановились без дублей. The manual mailbox
provider supplied scenario usage, not measured model usage. This verifies the
owned test stand, not production or competing external controllers.

Written by Codex (gpt-6/Codex desktop), Shuttle developer
on behalf of Daniel Drizhuk
