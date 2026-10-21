# Mini App startup lock, 2026-10-21

## Problem and change

During connection, the pass-payment controller can finish loading before later
modules start. Its normal completion calls the shared `setBusy(false)`, which
previously enabled logout, profile edits and payment controls while connection
was still in progress. This is a UI lifecycle race; this check does not establish
that it caused a business action or data loss.

The connection now holds its own boolean lock. Shared busy-state changes keep
controls disabled until the connection's `finally` block releases that lock.
No API contract, database schema or agent manifest changes are required.

## Verification

- The corrected behavioral test loads the actual pass-payment controller,
  verifies successful target/history reads, then holds the last module's startup.
  On the original application it produces exactly one expected failure: controls
  are enabled too soon. The other 32 tests in that file pass.
- On the corrected application, all 368 tests in the 16 CI UI suites pass.
- `go test ./internal/api` passes. Its first sandboxed attempt could not read the
  Go build cache; the subsequent permitted attempt passes.
- A separate headless Edge check uses the actual HTML, application and
  pass-payment assets. Logout, profile save and recent-payment controls stay
  disabled while the final module waits. They become enabled after success and
  after a simulated startup failure; failure also makes retry available. Neither
  case produces an uncaught browser error.
- The second independent review of the two-file code/test diff reports no
  actionable findings. The first review found a fixture-variable shadowing bug;
  the harness was corrected before the reproduction and successful checks above.

The initial test attempt allowed failed payment startup to go unnoticed and is
retained as incomplete evidence. It is not the accepted reproduction.

## Limits and retained evidence

The Edge check supplies synthetic HTTP/session replies and a deliberately delayed
last module. It verifies browser behavior, not Telegram WebView, IAM or Loom
integration. The fix is not yet installed on the owned primary; production was
not changed. CI for the preceding documentation commit does not validate this fix.

Ignored evidence is under
`.cache/shuttle-work/full-go-review-2026-10-21-v1/`: corrected red/green logs,
the Edge controller, screenshots and `startup-browser-proof.json`.

The wider security, recovery and quality reviews that identified this issue
inspected selected paths across the Go rewrite. Their lack of other findings is
not proof that every line of the supplied 728-file diff was reviewed.
