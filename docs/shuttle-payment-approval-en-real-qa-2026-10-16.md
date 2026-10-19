# English payment approval: real Telegram QA

The installed combined image passed a separate English approval/free-text path
in the actual @complynx_test_bot chat using the authorized current account.
Real Zitadel, Loom and the module broker were used with a manual provider.
This complements the [Russian case](shuttle-payment-approval-real-qa-2026-10-16.md).
It does not validate unattended model understanding or language selection.

A fresh synthetic PDF was rendered, visually checked and uploaded through the
Telegram chooser. A new assigned/unpaid pass and scoped reviewer role were
prepared through owned-stand APIs. The PDF was submitted as pending evidence.
The real broker prepared a workspace; no payment was credited before approval.
Telegram then showed "Confirm payment?", event and payer, 30.87 BYN credit,
123.45 BYN starting balance, 92.58 BYN remainder, deposit status and Europe/Minsk
deadline. Complete exact JSON arguments stayed in the approval message.

The actual free-text English approval continued the same review run.
interaction_resolve credited exactly one review: 3087 minor units, remaining 9258,
deposit satisfied and not fully paid. Telegram displayed "Response received."
and an English payment notification. Three accepted tasks completed across two
runs: file reception and review with steering. Bot language stayed English.
The same bot, poller and eight companions (including Loom) remain running.
No temporary containers or new runtime configuration were created for this case.

## Retained driver failure and recovery

The first completion check expected interaction_resolve output in the immediately
following model input. Loom had automatically compacted it. The write had already
succeeded; the assertion failure is retained and is not counted as a passing try.
We located the original matching call output in the earlier request, checked its
workspace/evidence and balance, then completed the same pending model step.
No interaction_resolve, payment write or Telegram user message was resent.
A follow-up diagnostic first assumed the latest provider response contained a
call, but it was a compaction response; its read-only IndexError had no effects.
Recovery then searched the actual call ID. This is a QA-driver assumption,
not a demonstrated Loom defect. The standalone driver preserves both paths.

The file-reception helper used a canned Russian reply even though its caption
was English. That text came from the manual provider; it is not evidence about
model language selection. The bot-generated approval/notification/acknowledgement
were visibly English. The [natural question expiry](shuttle-dialog-expiry-real-qa-2026-10-16.md) passed later.
Real approval expiry, protocol buttons, cross-person review, unattended model
behavior, interactive zitadeltg/CA and production remain open.

Evidence: ignored `.cache/shuttle-work/payment-approval-en-real-qa/checkpoint.json`,
matching original resolution output records and the standalone
`.cache/shuttle-work/payment_approval_en_real_qa.py`. API fixtures and direct reads
are distinct from actual Telegram upload and free-text transport. No publication
or production change occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
