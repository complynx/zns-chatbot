# Native payment authority through lock waits, 2026-10-21

The Mini App payment-review transport captured verified IAM privileges before
entering its database transaction. Unlike assignment and owner-evidence paths,
it did not retain a check for known local IAM invalidation or principal expiry.
A confirmation waiting on an event lock could therefore credit money after
invalidation, while the browser session remained active.

The transport now captures an invalidation fence before verification and retains
the verified principal's expiry. Native authorization rechecks it around session
lock waits; native prepare, cancellation and confirmation check again before
commit. These checks perform no IAM requests under database locks. The owner
evidence adapter carries its existing fence into the shared session check.
Agent-origin operations retain their existing behavior. No schema or HTTP input
change is required.

## Evidence and limits

- The new HTTP scenario observes the confirmation blocked on an event lock,
  invalidates the verified global administrator locally, then releases the lock.
  Original code returns 200; fixed code returns 403 without a financial review
  or confirmation receipt.
- Final frozen-source Linux/PostgreSQL/race checks pass 59 roots / 90 cases in
  two disjoint sequential runs. They include reviewer and owner happy paths,
  deposit/top-up evidence, receipt replay, native/agent handoffs and order reviews.
  Both final runners use 3 GiB with no swap and finish without OOM. Lint reports
  zero issues. Final independent source review reports no actionable findings.
- Earlier attempts remain recorded: two test-only session conversions failed
  compilation; an omitted callback in the owner-evidence adapter caused the
  existing owner HTTP happy path to return 403; a combined 2 GiB race run reached
  OOM. The conversions and adapter were corrected. The same expanded test set
  then passed in the two bounded runs; the OOM result is not accepted as success.
- An earlier review also found incorrect mapping of an early scope denial to
  temporary unavailability. That check now uses the existing identity-denial
  path. The event-wait test exercises the later transaction-denial path.
- All seven terminal runners, PostgreSQL, network and volume were removed after
  archiving logs and fully decoding a retained database dump. The owned primary's
  domain data, queue, Loom state and runtime epochs were preserved.

Evidence is under `.cache/shuttle-work/payment-review-authority-v1/`.
The HTTP peers simulate IAM and Telegram. This is not real Zitadel, Loom or
Telegram acceptance. Principal expiry and invalidation during every later SQL
operation were not independently exercised. Remote CI and primary installation
of this patch remain separate gates; production is unchanged.
