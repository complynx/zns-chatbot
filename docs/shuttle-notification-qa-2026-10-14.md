# Native notification QA — 2026-10-14

This acceptance uses our owned `shuttle-live-test` stand, application schema 14,
manifest mvp-12 and bot image
`sha256:0c70fd9fe49e48fa67ffc6f80d37a7f9dc3fb1b0a887984e3001b2cd49a39bb0`.
Loom uses the previously documented canonical-seal fixture. No developer stand or
production service was changed. A database snapshot was saved before migration.

Only the currently authorized Telegram account in @complynx_test_bot received
messages. The test event and monetary terms were explicitly synthetic; no real
payment was recorded. The source was the previously uploaded synthetic PDF, referenced
by its opaque file ID. This lane exercises notifications and exact approvals, not
receipt classification or bank-proof interpretation.

## Passed scenarios

Evidence submission produced one pending notice for the sole event authority,
temporarily assigned to the real test account. IAM and Loom were both paused after
the API accepted the evidence. The notice stayed pending without a send reservation.
Restarting the bot preserved the queue. Restoring both services delivered the notice
to the same account, with a persisted Telegram message/chat receipt. No money was
credited by submission.

A real Loom run under the local fixture administrator requested payment review.
Exact approval disclosed the evidence, recipient, amount, deadline and reason.
This pass had no credited money before approval. Approving the synthetic fixture credited
30.87 BYN once and delivered a decision notice. Telegram Web showed the Europe/Minsk
decision time, 92.58 BYN remaining, and an explicit deposit-satisfied/pass-not-fully-paid
status. System messages used the account's Telegram language, English; that is separate
from the agent's message-language selection.

For operator reconciliation, the already-delivered submission notice was changed
to an unknown-receipt state by a conditional SQL fault injection on this test stand.
The original receipt was preserved in the private proof. This simulates loss of a
durable receipt; it is not a native network-response-loss test. Because the message
was visibly present in Telegram, the operator chose dismiss rather than retry.
A real Loom exact approval executed dismiss, with no additional Telegram message
and no payment change. Its original status receipt and the single ledger record
survived another bot restart. The temporary event authority was revoked afterward;
the real Telegram account received no global IAM role.

## Evidence and limits

Private evidence is in ignored `.cache/shuttle-live-test/`:

- `native-notifications-proof.json`: scenario flags, receipts, approvals, ledger
  balance and explicit fault-injection description.
- `native-notifications-pass.jpg`: the actual deposit decision in Telegram Web.
- `pre-notifications-20261014.dump`: the pre-migration test database snapshot.

Logs and exit states are retained in ignored `.cache/shuttle-work/`. Initial failures
were retained: the helper confused Loom's artifact owner ID with the local account,
used the wrong approval-state name, expected a nested Telegram receipt, and read a
SQL UPDATE command tag as a version. Those helper errors were corrected without
repeating accepted business operations or restarting an unobserved Loom run.

One API request while IAM was paused returned 503 before evidence acceptance. It
created no payment evidence. The successful recovery scenario explicitly accepted
work before dependency downtime; it does not prove every new request can authorize
during an IAM outage.

Native retry was deliberately not used for the visibly delivered message. Last-resort
retry, rejection notices, revocation, delayed Telegram rejection, proposal expiry,
argument mismatch and receipt replay have focused HTTP-peer/PostgreSQL race coverage.
Native rejection and fully-paid top-up messages were subsequently verified in
[payment correction/top-up QA](shuttle-topup-qa-2026-10-14.md).
The full Linux/PostgreSQL race suite, zero-issue lint and independent source reviews
passed before this native deployment. No application code changed during native QA.

PDF visual rendering remains a failed gate (Loom issue #4). Interactive zitadeltg
linking still needs the deferred browser CA setup. This report does not establish
all roadmap stages, production readiness or production cutover.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
