# Pass notifications and deadlines

`passbooking.Service` appends registration, pair, assignment, waitlist, cancellation
and payment-review notices in the same PostgreSQL transaction as the mutation.
The outbox contains the recipient and subject separately. Invitations go only to a
known matching Telegram user. Payment review requests go to the receiving event
payment administrator; decisions notify affected participant owners.

The service-only delivery contract is `PendingNotifications(ctx)` and
`CompleteNotification(ctx, id, failure)`. Pending reads return at most 25 records,
one oldest outstanding record per recipient. Retryable failures delay that head
five seconds; permanent Telegram rejections finish it. A blocked recipient does
not block other recipients. Eligible recipient heads are ordered by availability
time, then ID, so matured retries cannot starve older untouched work.
Operation replay and per-generation uniqueness prevent
duplicate enqueue. The poller uses the existing single-process session lock.

`Notification.Current` is computed from live booking state, registration or
assignment generation, and current payment attachment/decision. Superseded
assignment reminders and replaced receipt requests must be acknowledged without a
send. A removed event payment administrator cannot receive an old review request.
The DTO carries current event titles. The delivery adapter selects its catalog
message and title using the recipient's current locale (EN/RU), then refreshes an
opened pass card. The domain never persists rendered localized prose.

The bot stores the Telegram message ID in `bot.pass_notification_deliveries` before
acknowledging the domain notice. This supports recovery after a lost acknowledgement.
As with existing order/massage delivery, a crash after Telegram accepts a message
but before its message ID is persisted can duplicate the send. There is no claim
of exactly-once delivery across that external boundary.

`ProcessDeadlines(ctx)` is trusted maintenance and needs no user actor. It processes
active events in keyset pages of up to 100, acquires the same event row lock as user/admin changes,
then reads `clock_timestamp()` before making decisions. Source parity is based on
`zns-chatbot/plugins/passes.py:34-39,1583-1669,3971-4049`:

- First payment reminder: assignment older than six days.
- Second reminder: first reminder marker older than one day.
- Cancellation: first marker older than two days. A twenty-day-old assignment
  discovered after downtime receives its first reminder and the full grace period.
- Pending invitation: expires after two days and ten hours, becoming solo waitlist;
  both inviter and a known invitee receive expiration notices. The queue recalculates.

The first marker records durable enqueue time, matching Python's pre-send marker.
Enqueue and marker updates commit atomically. Delivery outages do not postpone the
financial deadline after a reminder has been durably queued. Markers are keyed by
event, owner and assignment timestamp. Explicit repricing starts a new generation;
metadata-only admin edits retain the previous assignment/payment timeline.
Paid participants are never expired. A historical mixed paid/unpaid pair unlinks
the paid survivor without changing its price or proof attachment.

Real PostgreSQL tests cover recipients, transitions, operation replay, current
titles, pending ordering/backoff, restart, stale receipt/reminder suppression,
concurrent scans, marker-relative timing, invitation expiry, transactional enqueue
failure, free-pass safety and a paid survivor. Delivery/UI tests are separate and
must verify EN/RU rendering, Telegram failures, history and retry behavior.

Remaining source behavior: hype-thread registration announcements require event
channel/thread/locale configuration and a separate delivery integration. That
source path (`passes.py:4364-4419`) is not implemented by this owner-notification
slice and must not be counted as accepted parity.

The same startup/minute scan recalculates active events with waitlisted bookings,
including events with no due reminder. Dated tiers can therefore open without a
user action, matching Python's startup/timeout recalculation (`passes.py:3968,4043`).
Keyset paging visits each candidate event once per scan: permanently blocked queues
on the first page cannot starve later events. Closed events are skipped and the
finish time is checked again after locking. Assignment and owner notices use the
existing atomic transaction; repeated scans do not change an unchanged booking's
version or enqueue its waitlist/assignment notice again. A failed event rolls back
independently; the scan continues and returns a bounded error summary. Startup logs
that failure and the minute scanner retries, without taking unrelated API functions
offline. Cancellation still stops the scan and startup. The returned count remains
the number of deadline transitions, not queue assignments.
