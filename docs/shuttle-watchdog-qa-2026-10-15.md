# Watchdog subprocess acceptance — 2026-10-15

The existing production watchdog is now checked in real Linux subprocesses.
This adds behavioral tests; production code, configuration, dependencies and
runtime images are unchanged. Architecture and Go consultations preceded the
test implementation.

## Scenarios

The parent starts the compiled command-package test binary with a dedicated
helper selector and no application credentials. The child installs a SIGTERM
context and the actual `watchdog` function, then acknowledges readiness. Only
then does the parent send SIGTERM.

- Blocked cleanup: a deferred cleanup callback acknowledges entry and remains
  blocked before watchdog disarm, matching the production defer order. The
  watchdog must exit the child with code 1 after its five-second timer.
- Disarmed watchdog: after cancellation the child disarms the watchdog and
  acknowledges it. It must remain alive for six seconds, then exit successfully
  when the parent releases cleanup through stdin.

The parent has a twelve-second observation limit and reaps every started child,
including after an assertion failure. Expiry of that limit cannot pass as a
watchdog exit. Unexpected stderr also fails acceptance, so panic or race reports
cannot masquerade as success. The race runtime's exit sleep is disabled only in
these children; its distinct race-failure exit code remains enabled.

## Verification

The first focused Linux race run passed: blocked exit at 5.007257174 seconds and
disarmed release at 6.003052215 seconds. Inspection then made cancellation apply
to early setup failure and bounded stdout forwarding on failure. Strict lint
requested simpler function structure and explicit handling of the signal-owning
helper's nonparallel execution; a subsequent context-parameter ordering finding
was corrected. Final strict command-package lint reported zero issues.

The final Linux race run of the complete command package passed in 7.130 seconds:
blocked exit at **5.023623582 seconds**, disarmed release at **6.048246493 seconds**.
Both disposable test containers exited 0 without OOM and were removed. They had
1.5 GiB memory and two CPU limits. No new full integration or GitHub CI result is
claimed. Fresh independent correctness and adversarial quality reviews found no
actionable findings. The disarm check proves survival after disarm; it does not
synchronize with the internal timer and therefore does not separately prove
cancellation of an already-running timer.

## Limits and evidence

This proves behavior of the actual watchdog in isolated processes with synthetic
cleanup. It does not exercise a stuck database, telemetry exporter, worker or
HTTP cleanup in the complete application. The twelve-second parent limit is a
test guard, not an accepted shutdown SLA. Scheduler and process-observation
latency explain why observed exit can exceed the five-second timer; this test
does not assert termination at or before exactly five seconds.

The owned bot remains on the previously accepted
`sha256:c9ada93b9e6db29274f53adf3b556654b7c67486ef6d8bf0722e684d783233a5`
image. No service restart, Telegram message, business operation or IAM mutation
was needed. The earlier [healthy stop/start](shuttle-shutdown-qa-2026-10-15.md)
remains a separate observation. Full shutdown acceptance and external hard-kill
monitoring remain open.

Ignored logs and terminal states are under
`.cache/shuttle-work/container-results/zinusya-watchdog*-qa-20261015.*`.
Lint and the scoped change checkpoint are under `.cache/shuttle-work/watchdog-qa/`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
