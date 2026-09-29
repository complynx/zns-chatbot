# Order delivery and reminders checkpoint

Port the active notification paths from `plugins/orders.py`.

- Cash request and routed proof alert the selected administrator without
  requiring `/orders`. Accept/reject notify the customer.
- Capacity removal notifies the affected owner with removed services and the
  saved revised total. Manual and agent order mutations use the same path.
- API owns notification creation and authoritative recipient identities in the
  business transaction. Bot owns Telegram transport and delivery receipts;
  no direct access to Core tables. A narrow service token reads and acknowledges
  the delivery queue; user tokens cannot access it, and the delivery token cannot
  invoke user business actions.
- An API retry must not enqueue a second notification. Failed normal delivery
  remains pending; one blocked chat must not stop unrelated users.
- GUI buttons are generated from current authorized state. Delayed alerts must
  not reintroduce outdated action buttons. Notification history is visible to
  the same user's agent.
- Preserve Python's once-per-order overdue reminder rules: unpaid/cash, positive
  total, age at least the configured interval (default two days). A reminder is
  claimed before attempting delivery and is not repeatedly sent after failure.
  Distinguish this policy from retryable payment/capacity notifications.
- Add documented sandbox controls for time-dependent acceptance. QA needs to
  age its own test order and close/reopen the event without editing product code.
- Provide remaining-seat controls for capacity acceptance without filling the
  whole published capacity. Preserve existing reservations; reject an unsafe
  reset below the number already reserved.

Required proof: transaction/retry/concurrency assertions on PostgreSQL; separate
mouse/touch delivery flows; admin that has not opened `/orders`; mixed manual
and agent history; transient and permanently blocked destinations; restart
persistence; proof routing and late reminder suppression after payment.

Validation:

- Full local quality gate passed: Golden/Nebius Go lint, ESLint, Prettier,
  module verification, vet/build, PostgreSQL race tests, identity fuzzing, live
  contract checks and all five mouse/touch browser suites.
- Fresh Code QA pass 3: no actionable findings. A fixture reset edge case found
  during review was fixed and covered by a real PostgreSQL race test: default
  capacity cannot displace reservations created under a larger fixture limit.
- Independent Functional QA passed the exercised cash/proof/RU/deadline,
  reminder, recipient isolation and restart scenarios. Report:
  `qa.local/functional-notifications-pass1/report.md`.
- Separate capacity Functional QA passed the remaining-seat control,
  displacement messages, current cards, shared history and completed-delivery
  restart persistence. Report:
  `qa.local/functional-capacity-notifications/report.md`. Its three orders were
  removed and the catalog capacity restored; existing reservations preserved.

The stand uses scripted model responses and emulated touch. The first functional
pass did not target the exact Telegram-send/receipt crash window or lost Core
acknowledgment; focused integration tests cover acknowledgment retries. The
documented crash-after-send duplicate window remains a transport limitation.

This checkpoint is accepted. Both independent QA gates passed.
Orders XLSX export is the next product slice; full Python parity is not claimed.
