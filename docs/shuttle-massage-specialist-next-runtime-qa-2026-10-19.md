# Specialist upcoming-session runtime acceptance

Date: 2026-10-19. Accepted on an owned disposable stand; no primary or production
installation. Current candidate: schema52 / `mvp-59`, Linux/amd64 image
`sha256:532bdc8f3881b4572bbca9a52dc1880cc69c5d61a4208e1951009cf0cb16b8ff`.
Source hashes match the accepted component gate and the full Dockerfile build.
The dedicated builder used 2 GiB/no extra swap, 2 CPU and parallelism 1, then was removed.

## Scope and results

The nine-service `zinusya-mock-specialist-soon-r1` project had its own real Zitadel,
PostgreSQL, pinned owned Loom image, provider, mock Telegram and TLS certificates.
No Loom developer checkout/stand or real Telegram account was used. The provider
was manually driven through real Loom tools; this proves contracts and effects,
not unattended model intent recognition.

Two synthetic users were created through signed mini-app bootstrap. A fixture
administrator configured an active Europe/Minsk event, massage schedule and private
specialist binding. The specialist had no pass. Schema52, manifest `mvp-59` and
zero initial tasks/runs were checked before the workflow.

1. Actual Loom read preferences through `script`, then changed only `notify_next`
   to false through `module_call`, retaining `notify_bookings=true` at version 1.
2. The shipped mini-app in Edge loaded that state, enabled upcoming reminders and
   disabled booking/cancellation notices in one save. Reload preserved both values
   at version 2. Requests used the real backend and signed fixture identity; only
   the Telegram SDK bootstrap was replaced locally.
3. The next real Loom turn received the original native operation in its model
   context, read current preferences, then enabled only booking/cancellation
   notices. Both flags were true at version 3. Exactly three original preference
   receipts were retained across the two reactive tasks/runs and one native save.
4. A staff self-reservation generated a soon notice while suppressing the initial
   booked notice. Mock Telegram explicitly rejected its first send with 429. The
   owned Loom container was stopped during this deterministic notification test.
5. The bot stopped with SIGTERM in 0.765s, exit 0, without OOM. The same container
   resumed the original notice after the retry deadline: one accepted send, the
   same notice ID and unchanged original payload, binding and next-generation
   capture. The new attempt ID followed a known rejection. Contact entity and
   message receipt matched the original Bot API request. Loom was restored using
   the same owned container; no new agent run was required for delivery.
6. The first booking was cancelled. A minute-grid fixture then kept a fresh self
   booking due independently of inspection delays. Its soon send received 429;
   cancellation changed that original retryable notice to `delivery_blocked`.
   After the original retry time plus two seconds, its state was unchanged and
   there was no accepted soon send or matching message. Both fixture bookings
   ended cancelled. Only the original two reactive tasks/runs existed.

Owner long/short jobs remain separate and retain their current presentation.
This gate does not resolve the D29 coalescing/presentation policy question.

## Retained failure and limits

The initial delivery observer raised `KeyError: entities` after the successful
send because mock message snapshots omit entity echo. Original Bot API attempts
and the bot's persisted payload did contain the expected contact entity.
A separately reviewed read-back matched the original/current/accepted payloads,
one rejected attempt, one accepted attempt and the exact message receipt. It
restored only the stopped owned Loom container. No booking or message was replayed
to obtain a passing result. The original failed state and transcript remain.
The later [fixture entity echo fix](shuttle-mock-telegram-entities-qa-2026-10-19.md)
is accepted separately. This historical observer failure is not a Loom defect.

Real Telegram WebView, browser CA installation and zitadeltg acceptance remain
reserved for the agreed session with Daniel. Actual IAM/Loom plus mock Telegram
does not establish those results. The primary remains schema49 / `mvp-56`.

## Custody and cleanup

Independent build/runtime/reconciliation/cancellation reviews completed before
their respective executions. The sole actionable reconciliation finding added
comparison with the original pre-restart payload; a fresh review passed.
No production code changed after the source-frozen build.

Both app and Loom databases were dumped and validated with `pg_restore --list`;
service logs were retained. Their archives are 310178 and 100676 bytes. All nine
project containers, its volumes/network and the dedicated builder were removed.
The original eleven-container primary, installed configuration and business data
were checked unchanged; its Loom run count stayed 132. No global prune was used.

Ignored evidence: `.cache/shuttle-work/specialist-soon-images-v1-qa/` and
`.cache/shuttle-work/specialist-soon-runtime-v1-qa/`. The latter includes the
runtime checkpoint, actual model/provider records, screenshots, original failure,
`restart-reconciliation.json`, validated archives, `cleanup-proof.json` and the
final `source-gate.json`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
