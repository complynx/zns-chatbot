# Durable order notice delivery

## Implementation status

Source schema 45 / manifest mvp-44 implements delivery for order payment evidence
and explicit administrator recovery. Real PostgreSQL and HTTP Telegram/IAM/Loom
peers verified the component. On 2026-10-17, the owned main stand was updated
from schema 40 to schema 45 / manifest mvp-44. The same Telegram poller was
restored after real IAM owner reads. The native proof/review path passed later that day; see the
[native QA checkpoint](shuttle-order-proof-native-qa-2026-10-17.md). Agent/mixed,
reservation crash, upstream faults and browser download acceptance remain pending.
Production readiness is not established. Publication remains on hold.

Native proof forms, reviewer forms and reactive action history already have their
own source verification; see the [owner context](shuttle-miniapp-order-proof-context.md)
and [reviewer context](shuttle-miniapp-order-review-context.md). The stored delivery
lifecycle is described in [the store checkpoint](shuttle-order-notice-delivery-store.md).

## Owned activation checkpoint

The bot stopped with exit 0 in 0.70 seconds. A consistent private PostgreSQL dump
was retained before migration. Existing row contents were unchanged after schemas
41–45, allowing only the added nullable order-notification target column; all nine
new tables were empty. The candidate retained its registration-details key, mounts,
environment and security settings. Eight companion services, including Loom and
Zitadel, were unchanged. The original poller container completed a fresh cycle.

Real Zitadel token exchange authenticated owner profile, private-field presence,
orders and dynamic order-payment discovery reads. Reopening the embedded Telegram
Mini App showed Signed in and the new order-payment controls. This proves launch
and owner reads, not the remaining proof/review flows or unattended agent behavior.

The first activation verifier incorrectly expected static methods on a dynamic
manifest descriptor and stopped after candidate startup. Its failure was retained.
The corrected descriptor and authenticated discovery checks passed on the same
container, without repeating migration or recreation. The local cutover and its
owner probe passed independent review. The backup and original Compose chain
remain the rollback path; restoring a dump after ingress resumes requires preserving
any later accepted updates. No production deployment or publication occurred.

## Worker and business boundaries

Admission/review atomically retain one notice per evidence/recipient/kind. The
payer and current event authorities are deduplicated. Repair scans bounded UUID
pages and inserts River jobs containing only opaque notice IDs and versions.
Startup under the deployment instance lock quarantines send_inflight attempts as
delivery_unknown. Jobs restored for an already reserved generation also preserve
uncertainty. A delivered or unknown notice is not automatically resent.

Before a send, the worker checks local eligibility, verifies the recipient through
IAM, reads language preference, refreshes eligibility after that IO, then reserves
under event/account locks with an exact version. Owners do not need a current
assigned pass for historical notices. Other recipients need current event authority;
all recipients need the orders module and a nonrevoked account in this deployment.
A superseded admission notice becomes blocked. This does not change the business
receipt or claim state. Authorization can still change after the transaction commits;
no database transaction can atomically cover the Telegram network operation.

The message uses the retained evidence and original sold order terms. EN/RU follows
bot preference and the existing language fallbacks. Event IANA timezone is taken
from the order snapshot, with the offset for each instant, including DST. Names and
reasons are plain text. No Telegram file locator or credential is included.
Current reviewers receive a protected Mini App link using
`#order-review?event=UUID&evidence=UUID`; the link does not approve the decision.

- pending: evidence received, payment awaits human review, applicable resources held.
- exhausted: payer sees that places ran out; reviewer sees refund attention is needed.
  The payment must still be checked and no automatic refund is made.
- needs_decision: unresolved human case, no confirmed payment or held resources.
- confirmed: the human decision confirmed payment and consumed its held resources.
- rejected: that claim's holds released; no automatic order cancellation or refund.

Notice delivery/recovery never confirms money, creates a review, changes allocations,
repeats submission or starts Loom. D23 partial/cash/mixed-capacity policy remains open.
Known Telegram rejection/NotStarted may become retryable with backoff. A lost receipt
or ambiguous transport result becomes delivery_unknown. Telegram IO and result
persistence are shared with payment/pair notices, preserving the shutdown cleanup.

## Administrative API

`zinusya_administration` adds these deployment-scoped global-administrator reads:

- `order_notifications`: delivery metadata, optional state, UUID after and limit 1..20.
  Default is 20; follow next as after. No payload, file bytes or credentials.
- `order_notification`: explicit notice UUID, original evidence/order, attempt ID,
  current delivery state/version and Telegram receipt. The payload is excluded.

`resolve_notification` accepts `notification_kind=order`; omitted/payment and pair
retain their existing meanings. Prepare/write/status/reconcile use the existing
exact-parameter protocol, actor/operation binding, expected version and reason.
Only delivery_unknown, delivery_failed or delivery_blocked can be resolved.
A retry proposal for unknown delivery warns that Telegram may already have the
message: inspect the chat/receipt and retry only as a last resort. Retry creates
one new pending generation; the worker checks recipient access again. Dismiss
closes delivery without claiming receipt and remains possible with the module off.
Exact operation replay returns the original result after expiry or later delivery.

Migration 45 adds a typed order-notice FK and extends the exactly-one-target
constraint. It preserves payment/pair resolution rows. Down refuses retained
order proposals or receipts; export before intentional schema rollback. Migration
44 independently refuses to discard attempted notification delivery history.
No automatic rollback or production activation is part of this stage.

## Observability and verification

Queue metrics and worker duration include the bounded kind
`zinusya_order_notification`. Business outcome records use bounded
`order_notifications`; pair delivery also has its explicit component label.
No IDs, message text or credentials are metric labels.

The accepted Linux Go 1.27.2/PostgreSQL 17 race gate passed 140 roots /287 pass
events across integration, DB, task, API and observability, with 937 unchanged
frozen inputs, no failures/skips/OOM. It covers all five states and both audiences,
EN/RU/DST, pending work across service restart, repeated business receipt,
owner/reviewer deduplication, lost Telegram response, no automatic unknown resend,
exact authorized retry/dismiss and receipt replay, rollback history refusal,
role/module loss during IAM wait, IAM outage, known Telegram 429 retry and no
financial/proactive Loom effects. Existing payment/pair notification/recovery tests
passed with the shared send path. The original failing JSON-byte assertion and
lint output are retained separately; the fix compares JSON meaning.

The owner/ambassador producer fixture was then tightened so authority exists before
admission. Its focused race test and final integration lint passed. Product Go/SQL
bytes remained identical to the full gate; only that test file changed. The first
focused attempt's language setup was overwritten by normal file Intake metadata;
that fixture failure is retained and the final language assertion remains strict.

A later [worker subprocess crash/replay gate](shuttle-order-notice-process-qa-2026-10-17.md)
adds a real force-kill during an observed mock send. Startup quarantines the same
attempt; the exact crashed River job then replays and completes without resend or
financial effects. Its final three-root race run and integration lint pass. This
is separate from full executable graceful shutdown and real Telegram fault QA.

Scoped lint, sqlc compile and reproducible generation passed. Independent
correctness, quality and final source reviews reported no new actionable findings.
A baseline syntax objection was rejected using the Go 1.27 compiler and prior race
proof. Test containers and their exact labeled volume were removed; main container
fingerprints stayed unchanged. Cleanup's 30-second PostgreSQL observation expired
while a checkpoint was finishing; reinspection found natural exit 0 without any
restart or further signal. That is a retained cleanup observation, not an
application graceful-restart test failure.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
