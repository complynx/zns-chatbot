# Native pass payment evidence

## Compatibility

## Participant flow

`/payments` opens `#pass-payments` without calling Loom or consuming a pending
agent question. The account card also opens the selected pass. Menu navigation
uses the existing durable intake and Telegram outbox. English and Russian are
available.

Send the original file to the bot first. Telegram intake persists its file ID
before agent processing. Choose an owned assigned pass, load an accepted file,
and enter the amount and purpose explicitly. Currency, scale, price, tier,
deposit policy and deadlines come from the frozen pass, rather than browser
claims. The form displays dates in the event timezone. Decimal input is converted
to exact minor units; exponent notation, grouping, excess precision and unsafe
integers are rejected.

The ordinary attachment remains a reactive agent input. Opening the form does
not silently change its classification or suppress its queued Loom task. The
form can submit while Loom is unavailable once Telegram intake has accepted the
file. That does not imply zero Loom attempts for the separately queued attachment.

Submission shares evidence with the event payment reviewers. Pending or rejected
evidence credits no money. A current ambassador, event administrator or verified global administrator can
confirm an amount through the existing shared review workflow. After confirmation,
the form shows the remaining amount separately and supports explicit top-up.

The initial and final deadlines are frozen with the pass. Timeliness uses the
original Telegram sent time, not processing or review time. A timely proof can
be reviewed later. Late proof or a top-up before the required deposit is confirmed
needs an explicit human case reason before credit. Overdue payment does not
automatically cancel a pass or forfeit prior payments.

## Recovery and interface handoff

The browser saves an owner-bound operation ID and exact event/pass/file/amount/
purpose before sending. An uncertain response blocks new submissions and target
changes. Recovery reads the same operation; an explicit retry sends the same
request, including its original purpose. Missing status does not prove the write
failed. No bearer token, file bytes or consent are stored in this journal.

The original pending submission receipt remains immutable even after human
review. Current evidence state, review and balance are read separately. The
receipt wrapper also supplies the authorized event timezone. Recovery reads the
receipt before the current assigned-pass target: if that target is unavailable,
historical reads remain usable and new submissions remain disabled. Authorization
or module denial is not treated as a missing target. Invalid or unavailable
browser storage blocks new writes while keeping authorized server history readable.

Native and agent submission use the same financial engine, provenance rules and
original operation receipt. Pending or confirmed source files cannot fund another
pass or order. Rejected evidence can be resubmitted under a new explicit operation.
A native history marker is written atomically with the submission or explicit same-operation POST replay of an existing agent submission;
GET recovery remains read-only, and replay does not move the marker’s original time.

The next user-triggered agent turn receives bounded native submission locators,
not consent or confirmed-money claims.
No proactive agent task is created.
An
older queued attachment predates this history; before any new write the agent
must refresh current evidence and balance rather than blindly resubmit the file.
Actual incoming-media and later reactive-turn behavior are covered by component
integration tests.

### Agent recovery of a native submission

Source candidate `mvp-75` exposes `zinusya_payments.operation` to recover the
complete original receipt using the native operation locator supplied in the
next reactive context. The receipt is historical, not the current confirmed
balance. Current actor and event/evidence access are required; missing is unknown.
See the [receipt-read contract](shuttle-payments.md#original-receipt-reads).

## API and authorization

All participant routes are under `/app/pass-payments/` and require the current
Mini App session and owner. An administrator role does not grant access to another
participant through these routes.

- `GET targets` pages owned assigned passes, including fully paid history.
- `GET events/{event}/passes/{pass}` reads frozen terms and current balance.
- `GET events/{event}/files` pages accepted owner file metadata.
- `GET events/{event}/files/{file}/content` reads protected source bytes.
- `GET events/{event}/evidence?pass_id=...` pages current owner evidence.
- `GET events/{event}/evidence/{evidence}` reads current state and human review.
- `GET events/{event}/operations/{operation}` recovers the original submission.
- `GET recent` reads bounded native submission locators.
- `POST events/{event}/evidence` accepts only `operation_id`, `pass_id`, `file_id`,
  `amount_minor` and `purpose` (`initial` or `top_up`).

Operation IDs are opaque nonempty strings of at most128 bytes. The native form
creates UUIDs. The server rejects missing, duplicate, unknown, null or trailing
JSON claims. It derives currency and scale from frozen pass terms, or from the
original receipt when replaying an earlier operation.

Writes use one database lease and the existing operation, event, actor SHARE and
session lock order. IAM verification happens before database locks. Local
invalidation, principal expiry and captured Mini App expiry are checked across
waits and before insertion/commit. File reads recheck owner, session, source locator
and permissions after external Telegram IO.

Migration64 adds only the native action marker table and index. It does not import
old payments or replace the existing ledger. Down migration refuses to discard
nonempty native history; intentional rollback requires an export decision.
