# Specialist upcoming-session notifications

## Behavior

Python `zns-chatbot/plugins/massage.py` defaults `notify_next` to true and uses
`SLOT_BUFFER=5` minutes. The Go repair loop now captures due booked sessions in
pages of at most 100. It uses the existing specialist notice ledger and delivery
worker, with one `soon` record per booking. Partial capture and application
restart cannot create another record for the same session.

Capture checks the bot/organization, enabled massage module, active private
specialist binding and current account. Each insert rechecks these conditions.
Upcoming reminders cover all enabled events that have not ended; they are not
limited to the legacy single closest event. Overdue sessions catch up only until
their event ends. This retains late catch-up while preventing historical ended
events from generating a backfill. Messages show the frozen absolute start/end
in the event timezone, rather than promising an inaccurate remaining five minutes.

The preference is read when capture becomes due, so enabling it before capture
works. If it is disabled at capture, a blocked record consumes that session's
notice, matching the Python mark-before-setting-check behavior. Re-enabling does
not revive it. Pending notices require the captured binding and independent
`notify_next` generation at reservation; revoke/regrant or disable/re-enable
cannot silently refresh either generation. Cancellation blocks pending/retryable
soon notices. An event that has ended is rejected before reservation.

Staff self-reservation suppresses only the initial booking notice. Upcoming
notices still apply. Current own-client permission is required for the contact
entity; cancelled notices have no contact. Delivery uses the existing fresh IAM
check, durable send reservation, known-rejection retry and uncertain-result
reconciliation. Unknown sends are not retried automatically. A committed external
send reservation cannot be retracted by a later cancellation or permission change.
No proactive agent run is created, and a Loom outage does not block this delivery.
Owner long/short reminder behavior is unchanged; coalescing remains a separate
policy decision.

## Shared settings and native form

Both flags belong to the bot account across its specialist events. Reading or
writing requires a current private binding in the selected enabled event. A pass
or administrator role does not substitute. Defaults are both true/version0.
Agent `set_preferences` and native `/preferences` share CAS and immutable receipts.
Requests contain the current `expected_version` and at least one explicit boolean:
`notify_bookings`, `notify_next`, or both. Omitted flags stay unchanged. Same-value
writes retain the account version; exact replay returns its original receipt.
Changing one flag does not invalidate queued notices for the other flag.

The shipped form exposes both independent checkboxes, with English/Russian labels.
It retains one immutable unresolved request across reload and retries the exact
operation. Legacy booking-only recovery records are accepted without adding a new
flag. Recovery reads current settings after the original receipt, so a later agent
change is not overwritten. Session/account changes clear the displayed state.

## Migration and rollback

Migration51 adds enabled defaults and independent generations. It preserves old
booking values and backfills the booking generation from the old account version.
Old booking-only request hashes stay stable; old receipt JSON is not rewritten.
Replay/status/reactive context decode an absent `notify_next` as true.

Migration52 expands the existing notice kind constraint. Its Down refuses any
retained `soon` history. Stop the exclusive bot writer before rollback and export
or reconcile new history first. Migration51 Down also refuses new preferences,
independent generations or receipts containing `notify_next`. Do not run an older
writer against these schemas after new writes. Primary rollback was not attempted.
