# AUD-10: inbox head failure

Status: the blocking mechanism was reproduced through actual Bot.Run on 2026-09-30. Repair and independent acceptance remain open. The original inspection below predates that reproduction.

## Runtime reproduction — 2026-09-30

Root executed TestInboxPoisonUpdateRetainsChatOrderWhileOtherChatProgresses on real PostgreSQL. The complete three-update batch and received cursor4 were durable before the first handler outcome. Alice1 then repeatedly returned a propagated non-SQL order-read failure. Bob3 received no visible reply within the bounded observation, and his payload remained behind Alice1; Alice2 never overtook Alice1. The test failed on the intended progress assertions (exit1, 6.18s), not setup. Runtime cancellation/join completed.

Source: platform/integration/inbox_retry_test.go. Raw evidence: qa.local/go-resume-20260929/r48-inbox-isolation-20260930/red-1.jsonl. This is pre-fix evidence, not a passing acceptance result. R47 implementation was dispatched only after this result; fresh Code and Functional QA are still required.

## Evidence

- `platform/internal/bot/inbox.go`, `drainInbox`: selects the lowest `update_id` globally. Any `Handle` error other than the two durable terminal-plan errors returns without completing the update.
- `platform/internal/bot/bot.go`, `poll`: drains the inbox before calling `TG.Updates`. A failed head therefore blocks both later persisted updates and fetching another batch.
- `platform/internal/bot/bot.go`, polling loop: logs the error and retries after the polling interval. Retrying retains the same head; no per-update attempt limit or deadline is used by this path.
- `platform/internal/store/migrations/023_telegram_inbox.sql`: the inbox stores the update ID, payload and receipt time. Its current processing path has no durable retry or quarantine transition.
- `platform/internal/telegram/retry_after.go`, `DeliveryOutcome`: outgoing delivery already distinguishes recipient rejection, provider pause, deferred rate limits and uncertain transport outcomes. This is a wire-result classifier, not an inbox processing policy. Reusing it indiscriminately for arbitrary handler failures would be incorrect.

## Control-call provenance clarification — 2026-09-30

Source inspection of telegram/control.go and cmd/zns/product_delivery.go shows that production wires a durable ControlPacer. It gates getUpdates and getFile calls with the shared bot cooldown. The fixed outer polling interval alone does not prove wire retry_after is ignored: deferred admission may prevent a wire call. The first429 ControlError retains an APIError cause; a later local admission deferral has NotBefore but no APIError cause. Inbox failure accounting must distinguish such local deferrals from new conclusive handler failures, and preserve any SQL-origin error from recording the control outcome.

Integration setup(t) does not install this control pacer by default. Runtime429 acceptance must explicitly use the real pacer and reconstruct it against the same database. Independent-user delivery is not expected while a bot-global Telegram cooldown legitimately applies; use a non-Telegram failure to prove cross-chat poison isolation. Existing delivery tests remain the authority for outbound send/edit policies.

## Required repair proof

Reproduce a permanently failing update followed by an unrelated user's valid update. Prove that recovery preserves the failed payload and diagnostic state, permits independent work, and preserves ordering where events share an owner or conversation. Retry state and cooldowns must survive restart. Keep completed domain effects idempotent on replay; an uncertain send must not become a duplicate send.

Cover permanent recipient rejection, a valid structured `retry_after`, transient application failure, restart during retry, and unavailable PostgreSQL. Database failures must retain the agreed process-exit/supervisor-restart policy rather than being treated as poison user input. Existing terminal history/pass plans must remain safely acknowledged. Define the bounded retry and operator recovery behavior before implementation; do not silently delete a failing update or bypass permission checks.

This inspection does not close AUD-10 and is not Functional QA.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk
