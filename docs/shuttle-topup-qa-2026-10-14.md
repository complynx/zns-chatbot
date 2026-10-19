# Native payment correction and top-up QA — 2026-10-14

This follows [notification QA](shuttle-notification-qa-2026-10-14.md) on the same
owned stand, schema 14, manifest mvp-12 and recorded bot image. No application code
changed. Only the authorized current Telegram account in @complynx_test_bot was
used. All documents, event terms and money were explicitly synthetic.

## Passed path

One new PDF was uploaded through Telegram Web with a Russian caption requesting a
pending test top-up for an explicit event/pass. The file contains 92.58 BYN and
clear NOT A BANK DOCUMENT / NO REAL MONEY labels. The bot durably accepted its
opaque file ID and caption. The real owner-scoped Loom run used native file tools
to extract PDF text. Authorized artifact content matched the local source's byte
count and SHA-256. Reads used the Loom script API; writes used module_call.

The agent read the pass balance, then submitted pending top-up evidence under the
user token. The existing 30.87 BYN deposit stayed unchanged. Its Russian response
said the evidence awaited review and did not claim payment confirmation.

A fixture administrator's real Loom run requested rejection. Before exact approval
there was no review for this evidence. After approval, a rejection notice appeared
in Telegram with its reason and an explicit statement that no money was credited.
The confirmed amount remained 30.87 BYN; remaining amount remained 92.58 BYN. The
payment version advanced because rejection is an audited decision.

A new Russian Telegram request explicitly resubmitted the same synthetic document
after rejection. The owner-scoped agent read the previous rejection and balance,
then created a new pending evidence ID. There was still only one actual file upload.
The new evidence preserved the original attachment's timezone-aware sent_at instant,
rather than taking the later resubmission message time. No extra money was credited.

A real Loom exact approval confirmed the corrected top-up. The owner received a
notice showing 92.58 BYN confirmed, 0.00 BYN remaining and Pass is fully paid, with
the decision time in Europe/Minsk. The pass total was exactly
30.87 + 92.58 = 123.45 BYN. Its decision history contained two confirmed decisions
and one rejection, with version 3. After a bot restart, the original top-up operation
receipt, balance, decision counts and Telegram message receipt were unchanged.
Telegram Web still showed one final confirmation. No active test event authority
remained on the real account; the retained role=none record is audit data.

## Evidence and limits

Ignored `.cache/shuttle-live-test/native-topup-proof.json` records the source hash,
owner task/run IDs, rejection and confirmation approvals, resubmission evidence,
original timestamps, notices, balances and restart checks. Screenshots are
`native-topup-rejected.jpg` and `native-topup-fully-paid.jpg`. Logs and failure exits
remain in `.cache/shuttle-work/`.

The stand uses Loom's mock mailbox with manual Codex responses, as documented for
previous native QA. Real IAM, Loom authorization, file processing, broker approvals,
business transactions and Telegram delivery were exercised. This is not an
unattended provider evaluation or proof of arbitrary receipt classification.
The event/pass were explicit; ambiguity between multiple payment expectations
remains a separate agent acceptance case.

Retained helper failures exposed incorrect QA assumptions: a short file reference
does not include SHA metadata; read methods require script rather than module_call;
the task table has no response column; Z and +00:00 are different strings for the
same instant; revoked local authority retains a role=none row. Each continuation
used the existing run, accepted evidence or completed restart. No accepted write
was repeated to repair a helper assertion, and no application change was needed.

PDF text processing passed; PDF visual rendering still fails independently in
Loom issue #4. This report does not accept avatar/media classification, all payment
edge cases, paired registration, interactive zitadeltg linking or production cutover.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
