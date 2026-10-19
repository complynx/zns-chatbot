# Current payment expectations in reactive agent context

This slice supplies context for user requests. It does not start a run, send a
reminder, classify a file or perform a payment merely because an expectation exists.
The existing profile context still supplies a pending name request. Incoming pair
invitations are not included in this payment snapshot.

## Owner read

`zinusya_payments.expectations` reads only the authenticated owner's currently
assigned, unpaid passes in this bot/organization. Global administrators receive
their own obligations through this method, without an authority bypass. No user ID
argument is accepted. The read locks and checks the current local account binding,
then uses one repeatable-read transaction and connection for the snapshot.

The query filters out free/fully paid passes before pagination. Authority-only events
cannot displace an owner's expectations. Event registration-module availability is
checked, but sales closing, an elapsed deadline or event ending do not hide an
otherwise eligible unpaid pass. Expiry alone neither cancels it nor credits money.

Each item contains:

- Event ID/name and IANA timezone.
- Confirmed balance, remaining amount, immutable price/currency/scale, deposit
  requirement/satisfaction and balance version.
- Initial/final/current deadline, current `initial` or `top_up` purpose and `overdue`.
- Pending evidence count and `awaiting_confirmation`.

Only human-confirmed decisions contribute to money. Pending evidence does not
subtract the remaining amount or demand an automatic replacement upload. A fully
paid pass disappears even if another proof is still pending. The top-up deadline
applies only after the deposit requirement is established under the existing rules.

Deadlines retain their assigned absolute instants. The response represents them
with the event timezone's offset at each deadline, including DST, and includes the
IANA name separately. It does not replace the zone with a fixed current offset.

`POST /internal/agent/payments/expectations` accepts optional `after` pass UUID and
`limit` (default 5, maximum 20). It returns `observed_at`, `expectations`, `has_more`
and optional `next`. Order is stable pass UUID, not payment priority. Follow `next`
as `after` while `has_more`. Encoded items are capped at 32 KiB per page. Concurrent
changes can alter eligibility; refresh from the beginning for a new full traversal.

## Agent dispatch and recovery

The first page is appended alongside the existing profile/name context when a new
run or textual question answer/steering input is prepared. Preparation happens
before the outgoing request is checkpointed. Retries of an uncertain create/control
reuse the saved payload, including its original snapshot; a later user input gets
fresh context. Storage failure prevents dispatch of an incomplete prompt.

The prompt calls the snapshot untrusted context. It tells the agent to inspect actual
user text/file content independently of name/payment expectations, clarify multiple
plausible targets or matching amounts, and refresh current balance before a write.
An unrelated image may remain ordinary media or an avatar. The API still checks
ownership, module availability, versions, deadlines and human confirmation at execution.

## Current acceptance boundary

The owned runtime now serves manifest `mvp-21`; schema remains 22. Architecture and
Go consultations preceded development. Independent correctness, quality and
privacy/protocol reviews found no remaining issues. Focused behavior checks cover
pending/confirmed amounts, deposit-to-top-up transitions, overdue expectations,
owner isolation/revocation, pagination, one-connection reads, DST and saved retry
payloads. The final complete explicit-package race run passed (integration 224.723 s),
and the application linter reported zero issues.

The first complete run caught an obsolete discovery-test method count after the
new method was added. The test now checks exact method IDs for owner and event
authority; the failure log is retained. The subsequent focused and complete runs
passed. It was a test expectation failure, not a native payment failure.

Owned real IAM/Loom/Telegram acceptance used two synthetic assigned passes and a
pending name request. An unrelated message left them unchanged. A fresh ambiguous
PDF was read through its opaque file ID and Loom's PDF text tool; the run asked
which event to use. The textual answer continued that same run, refreshed current
expectations and submitted exactly one pending proof for the selected pass. The
other pass and name request remained unchanged; confirmed money stayed zero.
After an actual bot restart, the same proof and expectations were readable, with
no duplicate proof or review. Both event zones were present in the context.

The provider was the stand's manual Codex mailbox. This proves the real dispatch,
question/answer, file-tool, authorization and storage path; it does not measure an
unattended model's classification accuracy. No real money or production system
was involved. Avatar-versus-proof classification was not newly exercised with an
image in this native scenario; focused behavior and prior avatar QA are separate
evidence. Browser CA/zitadeltg and production acceptance remain separate gates.

No dependency, migration, new waiting table or proactive scheduler was introduced.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
