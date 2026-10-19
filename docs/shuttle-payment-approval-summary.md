# Readable payment approval summaries

A newly prepared payment review now asks a question in the reviewer's selected
bot language. In automatic mode, Telegram ru/be/uk maps to Russian; other
languages map to English. The bot default is used when Telegram has no language.
The summary shows the event name and payer, decimal amounts with the frozen pass
currency and scale, the balance before and after the proposed decision, deposit
requirements, and proof timing in the event's IANA zone. A rejection credits zero.
It does not describe the payment as confirmed before the required decision.

Amounts use integer arithmetic. Event/payer/reason metadata is quoted and placed
in a literal block; embedded Markdown cannot create links, emphasis or mentions.
The complete exact argument block remains available in the approval message.
This changes presentation only: permissions, argument hashes, workspace revisions,
expiry and payment effects keep their existing rules.

The description is stored with the preparation. Repeating its original operation
keeps the original description, arguments, expiry and proposal ID, including
after a profile or language change. Earlier stored preparations retain their old
text. This is a schema39 source change with no data migration.

## Verification and acceptance boundary

The bounded Linux/race gate passed 72 integration roots / 107 pass events,
one exact-money unit root/event and 13 task roots / 42 events: 86 roots and
150 pass events in total, with no failures or skips. All 851 frozen inputs and
the ten main stand services were unchanged. Strict Go lint and sqlc compile passed.
Independent ordinary and quality reviews plus fresh final source reviews found
no actionable defects. The focused gate exercises prepare/replay/locales,
literal metadata, native/session/agent review, permissions, notifications,
reply keyboards, outages, restarts and delivery uncertainty with real disposable
PostgreSQL and mock peers. It is not new real-Telegram acceptance.

The first new gate failed because the test compared the dynamic HTTP server port
inside `$schema` between helper calls. The final test excludes only that schema
address and compares every remaining response field. The failed run is retained
in `.cache/shuttle-work/payment-approval-summary-qa`; it is not a passing attempt.
Earlier compile/lint failures in new tests are retained in the lint receipts.

Evidence is in ignored `.cache/shuttle-work/payment-approval-summary-v2-qa`,
including the exact expected roots, source freeze, race events and checkpoint.
The [keyboard acknowledgement fix](shuttle-approval-keyboard-ack.md) is the companion
source change. Both are now installed together on the owned stand as
`sha256:e6c429ba15abb5cc5782ddb066581cda6456a41f70be96e8e37db714aedd2170`. The exact release image passed an isolated
schema39 no-op migration with every existing table preserved. Image-only
activation kept the existing key, configuration and eight companion services;
real-IAM owner reads passed and the same Telegram poller completed a fresh cycle.
The dedicated builder and snapshot containers were removed. These deployment
checks alone do not prove visible messages. The subsequent
[real Telegram Russian happy path](shuttle-payment-approval-real-qa-2026-10-16.md)
now verifies the summary and free-text acknowledgement. The separate
[English path](shuttle-payment-approval-en-real-qa-2026-10-16.md) passes too.
The [natural question expiry](shuttle-dialog-expiry-real-qa-2026-10-16.md) now passes.
Real approval expiry and protocol-button checks remain open.
Local evidence: `.cache/shuttle-work/payment-approval-images-qa`,
`.cache/shuttle-work/payment-approval-schema-qa` and
`.cache/shuttle-live-test/payment-approval-main-cutover`.
No commit, publication or production change is part of this acceptance.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
