# IAM-first continuous-bot recovery QA — 2026-10-18

The original task/run/session recovered after overlapping Zitadel and Loom
outages, with IAM restored first. After one cold start while both dependencies
were unavailable, the bot remained the same process through both restorations.
The independent TLS provider stayed the same process throughout. No second bot
barrier, provider restart, replacement run or operator Resume was used.

## Actual path

A fresh isolated `iam-first-continuous` project used pinned local `mvp-56`/schema49,
frozen Loom fc6c574, its own real PostgreSQL/Zitadel/Loom, CA, fake Telegram
credentials and counted-provider mailbox. Fake Telegram update 7401 was accepted
with 202 and created one real user/inbox/task/delegated session/run. The first
model request reached the provider and remained unanswered. There was no outbox,
message send or Loom business write.

The bot stopped once. Loom then stopped gracefully, retaining the original run
as `paused`/`shutdown`. Its original model step became completed with
`platform_paid_unknown`, and its original sponsored attempt retained status
`unknown` and positive reserved exposure. Zitadel stopped next. PostgreSQL and
the independent provider remained available.

The same bot container cold-started while IAM and Loom were both down. Its
empty-cache authorization attempt left the same durable task in
`awaiting_authorization`, with the original inbox/user/run/session IDs intact.
After IAM returned first, that same bot process advanced to `awaiting_service`
while Loom remained stopped. The original run stayed shutdown-paused, and no
new inference, outbox or Telegram send occurred.

The same Loom container returned last. The bot automatically resumed the same
run after its typed shutdown pause. Exactly one new model request reached the
same provider. Only this replacement inference received one manually supplied
final answer. The old request file stayed unanswered.

Final assertions and retained terminal row evidence show one completed task,
one completed original run, one unchanged delegated session ID, one delivered
outbox row and one send attempt/visible bot reply. There are exactly two
completed model steps. The original unknown step is identical to its shutdown
snapshot; its sponsored-attempt step/run/request IDs, status and reserved
exposure are also unchanged. No Loom `write_operations` row was created.
These synthetic provider/accounting records do not establish an external charge
or financial settlement.

## Review, timing and preservation

The QA driver received independent correctness and safety review loops before
runtime effects. Verified fixes strengthened the original-attempt/step comparison,
pinned the first cold-bot epoch, rechecked continuity at final acceptance, and
fenced all nine service epochs. The final fresh reviews were clean. Fifteen
source/configuration hashes and immutable runtime image IDs were pinned. These
are frozen-image runtime checks, not a new image build or an unattended model test.

The initial bot stop plus inspection took 0.938s; Loom 0.875s; IAM 0.765s. The bot's
separate application drain log reports 2.403281ms with a 1000ms budget. The Docker
measurements include stop-command and inspection overhead. All intentional stops
exited 0 without OOM. Six companion process fingerprints, including the provider,
were unchanged. The cold-bot StartedAt was pinned immediately after its first
successful start and stayed unchanged through final acceptance.

Primary bot/poller epochs and 132 Loom runs were unchanged. All 112 fingerprints
matched except River leader/queue bookkeeping; all 46 River job rows were
identical. No retention exception or primary business change was needed.

## Evidence and cleanup

Before retirement, the application archive was 298651 bytes, SHA-256
`3be1ec744b31771f35bf10e900b04df06c362fc56021f1568b6bb82f5440d408`;
the Loom archive was 91578 bytes, SHA-256
`da71cc07500b96ecc1c784b47ddc5ed0f042e40b6b3c8a56fb78adf0ce287753`.
Ignored evidence under `.cache/shuttle-work/iam-first-continuous-v1-qa` retains
the source gate, uncertainty journals, original/final task and model records,
terminal rows, transcripts, logs, runtime fingerprints and preservation checks.
The owned project was destroyed after archiving; zero containers, volumes and
networks remain. No real Telegram message was sent.

Together with the [reverse-order gate](shuttle-existing-run-reverse-outage-qa-2026-10-18.md),
this proves both specified dependency-return orders. It does not cover actual
running-task delegated-session expiry, revocation during caching, abrupt Loom
kill, unattended inference or production readiness. Those gates remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
