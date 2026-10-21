# HTTP requests during graceful shutdown

The production HTTP server previously used the process signal context as its
request base context. SIGTERM cancelled active handlers before HTTP drain began,
including context-aware database work and avatar subprocesses that could have
completed within the remaining shutdown budget.

The HTTP base context now preserves its values without inheriting signal
cancellation. Shutdown stops admission and drains existing requests. When the
budget expires, the existing server close path still closes connections and
cancels unfinished request contexts. Worker and hard watchdog bounds are unchanged.

## Verification

The existing drain test now uses the production `server` helper and a handler
that observes request cancellation. Cancelling the lifecycle before drain made
the original code return `cancelled before drain` instead of `finished`.
The fixed code completes that request and the workers, then closes admission.

The complete `cmd/shuttle` Linux race gate passed six scenarios / eight cases.
The standalone `TestWatchdogHelper` entry is intentionally skipped; its blocked
and disarmed subprocess cases run through `TestWatchdogProcessExitAndDisarm`.
The gate also covers expired-budget connection closure and private-error-safe
shutdown logging. The final changed-package lint gate reported zero issues.
An independent source review reported no actionable findings.

The initial Windows race attempt could not start without CGO. The subsequent
Windows behavioral reproduction failed on the expected cancellation result,
and the fixed package passed locally. Linux race checks provide the target
platform evidence. A later lint gate required formatting of the test handler;
the final Linux gate includes that version.

This verifies server wiring and drain behavior. Lifecycle cancellation models
the signal path; it does not claim a new full-process SIGTERM acceptance on the
installed main stand. No main-stand image was changed.

Local evidence: `.cache/shuttle-work/http-drain-context-v1/` and
`.cache/shuttle-work/payment-session-expiry-v1/linux-main-v2/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
