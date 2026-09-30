# Durable Telegram intake

The single bot poller stores each received batch in `bot.telegram_inbox` in one
PostgreSQL transaction. That transaction also advances `telegram_received`, the
offset sent to Telegram. No handler runs before commit. Duplicate update IDs
retain the first payload. The single-poller guard does not replace deployment
coordination: stop the old main process before starting its replacement.

## Pending work and retries

Each pass handles at most 100 due pending rows. An earlier pending row blocks
later rows of the same private chat, including while it is cooling or parked.
Unrelated chats can progress. Unsupported updates without a chat key do not share
a barrier. Handler authorization, idempotency and delivery reconciliation still
apply; inbox durability does not provide exactly-once Telegram delivery.

Before dispatch, the poller persists a restart lease. An interrupted process leaves
the payload and lease intact without counting an unknown outcome as a failure.
Recorded ordinary handler failures use delays of 5 seconds, 30 seconds, 2 minutes
and 10 minutes. The fifth recorded failure quarantines the row and releases its
chat barrier. Malformed or mismatched updates are quarantined without dispatch.
Quarantine retains the original payload and fixed diagnostic codes; handler error
text is not stored in those diagnostic fields. Quarantined rows never auto-replay.

Database failures, parent cancellation and credit configuration failures stop the
pass without consuming this budget. Bot-wide Telegram pacing and structured 429
responses defer without consuming it. Valid absolute deadlines are not shortened
to an hour or converted through a bounded duration. Missing pacing uses a fallback;
invalid or unrepresentable pacing parks the pending row at timestamp `infinity`.
A later healthy upstream response does not automatically unpark that row.

Success removes the payload and advances `telegram` atomically. This processed
cursor is a high-water mark, not proof that every smaller update completed:
cooling, parked or quarantined rows can remain. Operational completion checks must
inspect the relevant inbox rows and delivery state, not the cursor alone.

## Inspection and recovery

Inspect metadata first, without exposing payloads in logs or support reports:

```sql
SELECT update_id, state, failures, failure, next_attempt_at, quarantined_at,
       next_attempt_at = 'infinity'::timestamptz AS parked
FROM bot.telegram_inbox
WHERE state = 'quarantined' OR next_attempt_at > clock_timestamp()
ORDER BY update_id;
```

`state='pending'` with `parked=true` means no automatic retry, and still blocks
later rows of that chat. `state='quarantined'` is terminal and no longer blocks it.
Finite future timestamps represent cooldowns or restart leases. Retained payloads
remain sensitive; quarantine is not an authorization to keep or expose them
outside the existing data policy. This change does not add a user deletion API or
alter history deletion behavior.

There is no general replay or unpark command. For a bounded manual recovery, stop
all inbox writers and identify the exact affected update and cause. Fix the cause,
then verify current sender authority, earlier pending chat rows, committed business
receipts and possible unknown outbound sends. Reconcile ambiguous effects before
retrying. A reviewed one-row unpark must preserve the payload and failure history,
match the observed pending state and deadline, and only make that row due. Restart
with a compatible reader and verify the resulting business and delivery state.
If safe replay cannot be established, keep the row parked and escalate for an
explicit disposition. Do not mass-reset failures, unquarantine rows, or replay
payloads merely because the provider recovered.

## Upgrade and rollback contract

Migration 087 is additive, but its reader contract is not backward compatible.
Stop all old writers before applying migrations and starting the new binary.
Verify that every active poller honors `state` and `next_attempt_at`.

**A plain rollback to the old binary is unsafe once cooling, leased, parked or
quarantined rows exist.** The old reader ignores these fields and can immediately
replay retained payloads, including terminal quarantine. Keeping the columns does
not make that rollback safe. Dropping the columns or deleting retained rows is not
a safe downgrade procedure.

Use a compatible hotfix that preserves these reader rules, or leave writers
stopped until a separate, reviewed data-preserving recovery plan establishes how
each retained row and its effects will be handled. Do not start an old reader
against this inbox as an emergency shortcut. Preserve the database and payloads
for recovery under the applicable privacy policy.

Focused tests cover durable receive, chat ordering, bounded failure recording,
restart leases, service deferral and retained quarantine. Current code and runtime
acceptance remain subject to the stage's independent Code QA and Functional QA;
this document does not claim production or cross-platform acceptance.
