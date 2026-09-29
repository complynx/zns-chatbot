# Durable Telegram intake

First implementation; independent QA and crash-process acceptance are pending.

The single bot poller stores every received batch in `bot.telegram_inbox` in one
PostgreSQL transaction. The same transaction advances `telegram_received`, the
offset sent to Telegram. No business handler runs before that transaction commits.
Duplicate update IDs retain the first stored payload.

The poller drains pending events in update order before fetching another batch.
A failed or cancelled handler leaves its event and successors in the inbox.
Startup resumes those events even if Telegram no longer holds the acknowledged
batch. Handlers retain their existing authorization and idempotency checks.

After a handler succeeds, deleting its payload and advancing the existing
`telegram` processed cursor happen atomically. The processed cursor remains the
public sandbox completion signal. Completed inbox payloads are removed rather
than becoming another indefinite store of personal data.

Shutdown may cancel processing immediately: already received durable work stays
pending. A bounded graceful drain and consolidated single-process lifecycle are
separate remaining tasks. This does not claim exactly-once Telegram delivery;
an ambiguous outbound response still needs delivery reconciliation.

Current integration evidence covers interruption during the first event of a
batch, replay after removal of acknowledged upstream updates, rollback when a
later batch insert fails, and replay after a committed business mutation with
inbox completion interrupted. Tests use real PostgreSQL and synthetic Telegram.
An additional test kills the actual processing subprocess without cleanup, removes
acknowledged upstream updates, and verifies recovery from PostgreSQL. It passed
on Windows. Independent Functional QA remains required; full Linux race acceptance
is blocked by separate deadline and large-order-list failures under investigation.

The current sequential loop preserves conversation order but a failing event
delays all later events. There is no distributed worker coordination. The existing
single-poller guard prevents accidental concurrent ownership; deployment must
still stop the old main process before starting its replacement.
