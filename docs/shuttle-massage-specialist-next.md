# Specialist upcoming-session notifications

Date: 2026-10-19. Current source is schema52 / `mvp-59`. Preference APIs,
timed delivery and the native checkbox pass component QA and
[owned real IAM/Loom runtime acceptance](shuttle-massage-specialist-next-runtime-qa-2026-10-19.md).
The Linux candidate is built and remains uninstalled. The primary stays schema49 /
`mvp-56`. Runtime QA uses a manual provider and mock Telegram; real Telegram
WebView and joint CA/zitadeltg acceptance remain separate.

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

## Verification and limits

The preference foundation previously passed 16 PostgreSQL roots. The timed-delivery
stage passes 35 integration roots with real PostgreSQL and fixture IAM/Loom/Telegram
HTTP peers, including both preference interfaces, stale CAS, original receipts,
cancellation, preference and binding generations, scope/event deadlines, staff
self-booking, unknown-send recovery, safe rollback and worker delivery while Loom
is unavailable. Configured DB/task/API/integration lint reports zero issues.
Targeted manifest/time-formatting unit tests pass. SQLC 1.31.1 compile/generation
passes and repeated generation leaves generated files unchanged.

All 26 specialist UI tests pass. Actual shipped HTML/JS also passes headless Edge
QA with a fixture HTTP API: independent toggles, Russian labels, lost response,
reload and exact original-operation retry. Rendered screenshots exposed a checkbox
width issue, which was fixed and rechecked. That fixture browser gate alone is not real Telegram WebView or IAM/Loom proof;
the separate runtime gate above provides actual IAM/Loom evidence. Independent scoped source/UI/quality reviews found
no actionable findings.

The first integration run passed 30 roots; its test-only PostgreSQL exceeded the
Docker stop deadline and exited137 without OOM. Its dump/logs remain archived.
The expanded run passed 35 roots and PostgreSQL shut down cleanly with exit0.
The `docker exec pg_ctl` observer ended when container PID1 exited, so cleanup was
accepted by independent terminal read-back, dump validation and preservation checks;
tests were not repeated to hide that observer failure. Both containers/volumes
were removed. The first browser loss simulation was transparently retried by the
browser and did not expose unresolved state; the retained replacement uses a
post-commit 503 and verifies exact replay. The existing retained-window UI test had
an outdated zero-configuration-read expectation; its failure was reproduced on the
pre-change source before correcting it to the current single read.

Ignored evidence: `.cache/shuttle-work/specialist-soon-delivery-v{1,2}-qa/`,
`.cache/shuttle-work/specialist-soon-delivery-v1/`, and
`.cache/shuttle-work/specialist-soon-checkbox-browser-v{2,3}-qa/`.
Successful fixture schemas are dropped by the harness, so the post-success dump
is not a business-data backup. The primary eleven-container runtime, installed
configuration and business data are preserved. No real Telegram messages were sent.

The candidate now passes the separate real IAM/Loom/mock-Telegram gate, including
both native checkboxes, actual Loom read/write calls, native receipt context,
known-rejection recovery across a normal bot restart, and cancellation before retry.
The nine-container QA project and its builder were archived/removed as applicable.
The runtime report retains an additional observer failure: mock message snapshots
omit entities, so original Bot API attempts and immutable payload/receipt data were
used for no-replay reconciliation. No production or primary installation occurred.
The subsequent [mock entity echo fix](shuttle-mock-telegram-entities-qa-2026-10-19.md)
passes its complete Linux fixture gate. Continue remaining infrastructure work. Joint real WebView/CA/zitadeltg acceptance stays reserved for Daniel.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
