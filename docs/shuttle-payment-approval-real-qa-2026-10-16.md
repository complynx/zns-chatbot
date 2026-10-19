# Real Telegram approval summary and acknowledgement QA

The combined image `sha256:e6c429ba15abb5cc5782ddb066581cda6456a41f70be96e8e37db714aedd2170` passed the
Russian approval/free-text happy path in the actual Telegram Web chat with
@complynx_test_bot, using only the authorized current account. Real Zitadel,
Loom and the module broker were used. The provider was driven manually; this
proves transport and state behavior, not unattended model interpretation.

The bot language was changed from English to Russian through the real embedded
Mini App. Its expired old session requested reopening, and a freshly opened
Telegram session saved the setting. A new visibly synthetic PDF was rendered,
visually checked and uploaded through Telegram's file chooser. File reception
completed without a business call. A fresh assigned/unpaid synthetic pass and
scoped reviewer role were created through APIs on the owned stand.

The review broker saved a workspace and requested exact approval. The actual
Telegram message showed "Подтвердить оплату?", the event name, payer, decimal
30.87 BYN credit, 123.45 BYN starting balance, 92.58 BYN proposed remainder,
deposit terms and Europe/Minsk proof timing. Full exact JSON parameters remained
in the message. There was no credited payment before the next user answer.

A real free-text Telegram approval continued the same Loom run and was resolved
through interaction_resolve against its input ID, approval ID and argument hash.
The new cleanup message visibly said "Ответ получен." The earlier misleading
expiry warning was not emitted for this answer. Exactly one review credited 3087
minor units, leaving 9258; the pass has a deposit and is not fully paid. Three
accepted tasks completed across two runs: file reception and review with steering.
The bot language was restored to English through the actual Mini App.

## Retained setup failure and limits

An initial attempt reused a PDF already submitted in an earlier payment case.
The source uniqueness guard rejected it. The rejected original body and response
are retained; the QA assertion failure is not a successful submission. We used a
fresh PDF and a new operation for the actual case. No existing payment was
repeated, undone or relabelled as this run. The old session-expiry observation
and browser locator/animation failures are also separate from successful UI
steps; no duplicate Telegram message was sent as a retry.

The exact bot, poller and eight companion service instances (including Loom)
match the accepted activation receipt. No extra containers were created for this QA.
Only synthetic business records and bot language versions changed. Original
language was restored. This is same-account payer/reviewer acceptance; it does
not prove cross-person review. The separate
[English case](shuttle-payment-approval-en-real-qa-2026-10-16.md) now covers English
approval and acknowledgement. The [natural question expiry](shuttle-dialog-expiry-real-qa-2026-10-16.md) also passes.
Real approval expiry and protocol buttons still need their own Telegram checks.
Component tests are not substituted for those checks. Production,
unattended model interpretation and interactive zitadeltg/CA remain open.

Evidence: ignored `.cache/shuttle-work/payment-approval-real-qa/checkpoint.json`,
its standalone driver, synthetic PDF and preview; activation evidence is under
`.cache/shuttle-live-test/payment-approval-main-cutover`. Signed Telegram data and
credentials are not included in this report. No commit, push or production
change occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
