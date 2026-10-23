# Recovering Loom shutdown pauses

Actual Loom preserves a run interrupted by process shutdown as paused with reason
shutdown. Polling alone does not queue it again. Shuttle resumes that existing run
through POST /v1/runs/{id}/resume with the existing delegation and a durable key.
It creates no replacement run, auth session or accepted Telegram task.

The key includes task ID and the observed positive pause version, and is saved
before dispatch.
Unknown transport outcomes retry the same key after re-reading
the run.
Observing the run progressing clears the pending resume; a later shutdown
uses a new operation key for its new version.
An HTTP 409 waits for Loom reconciliation
rather than terminalizing the accepted task or bypassing an unknown inference.

Automatic resume is dispatched only after observing paused/shutdown. Client pause, authorization,
provider retry exhaustion, billing and other reasons do not trigger resume. Local
revocation and token checks still precede calls. Paused or reconciling runs are
represented locally as awaiting_service unless they expose a question or approval.

This handles continuation of an already requested task. It does not authorize
proactive agent work or unknown-effect replay. Loom owns its provider ledger and
reconciliation guard; Shuttle cannot settle unknown model charges automatically.

## Concurrent control limitation

Loom resume accepts an empty body and does not atomically check the version or
pause reason observed by Shuttle. A concurrent external controller can change
shutdown to client_pause between the read and resume. Another read cannot close
that race. Strict preservation under competing controllers requires a Loom
expected-version/reason precondition. Until that exists, automatic recovery
assumes Shuttle is the sole run controller. The excluded-reason tests prove
observed reasons, not a concurrency guarantee.
