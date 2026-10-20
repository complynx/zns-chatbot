# Registration and pass workflow acceptance plan

The current-pair membership stage is installed and accepted within the limits in
[its report](shuttle-native-pair-membership.md). This next gate covers complete
participant registration and pass workflows. Existing component tests and seeded
pair fixtures do not establish that complete workflow.

## Existing paths and boundaries

- `/register` lists available events and presents role buttons. Its callback
  binds the selected event/version, owner, delivered prompt and expiry.
- Required legal name/passport fields create a shared durable expectation and
  link to the Mini App. Saving fields does not register the participant. A fresh
  role choice explicitly continues registration.
- `/profile`, name entry and legal registration details are separate fields.
  Legal name and passport may currently be changed at any time, as agreed.
- `/registrations` and the account form show the current owner's registration,
  pass, confirmed amount, pending evidence and remaining balance.
- Payment review has an event-authorized native form. Its presence does not prove
  an agentless participant evidence submission or native administrator assignment.

Source entry points are `internal/task/structured_register.go`,
`internal/task/structured_details.go`, `internal/task/structured.go`,
`internal/api/miniapp.go` and `internal/api/miniapp_payments.go`.

## Acceptance matrix

Record each path separately as passed, failed or not accepted:

| Workflow | Native only | Agent only | Native to agent | Agent to native |
| --- | --- | --- | --- | --- |
| Profile display name as a separate workflow | Passed, Loom stopped | Passed, controlled provider | Passed, controlled provider | Passed across restart, controlled provider |
| Required legal fields through solo leader waiting-list registration | Passed, Loom stopped | Passed, controlled provider | Passed, controlled provider | Passed across restart, controlled provider |
| Pair joining followed by pass assignment | Passed in joined native journey, Loom stopped | Passed in joined agent journey, controlled provider | Passed in joined mixed journey, controlled provider | Passed in joined mixed journey, controlled provider |
| Partial pass payment, review and top-up | Passed in joined native journey, Loom stopped | Passed in joined agent journey, controlled provider | Passed in joined mixed journey, controlled provider | Passed in joined mixed journey, controlled provider |
| Whole registration, pair, assignment and final payment | Passed, Loom stopped | Passed, controlled provider | Passed in joined mixed journey, controlled provider | Passed in joined mixed journey, controlled provider |

These cells describe the new complete-workflow gate, not a claim that earlier
component tests or narrower runtime checks failed. The [native runtime gate](shuttle-registration-native-runtime-qa-2026-10-19.md)
accepts required legal fields through solo waiting-list registration with Loom
stopped. The [agent/mixed runtime gate](shuttle-registration-mixed-runtime-qa-2026-10-19.md)
now accepts the other three paths with real Loom and a controlled provider.
This covers one leader happy path per interface combination, not every role,
profile field, authority or payment variation. The native joined journey now passes as described below. The [joined agent journey](shuttle-joined-agent-runtime-qa-2026-10-20.md) also passes
from fresh participants through final payment. Both [whole mixed happy paths](shuttle-joined-mixed-runtime-qa-2026-10-20.md) now also pass on candidate mvp75. The [mvp77 terms fix](shuttle-pair-assignment-terms-2026-10-20.md) has separate component and compatibility acceptance; [mvp77 primary installation](shuttle-pair-terms-primary-qa-2026-10-20.md) now also passes.

The [payment gate](shuttle-native-pass-payment-runtime-qa-2026-10-19.md) accepts
all four financial paths from assigned fixture passes, including actual human
review and top-up. That earlier payment-only gate uses API-assigned fixture
passes. The native cell now also has the complete joined-journey acceptance below.
The whole registration-through-assignment-through-final-payment journey is
accepted for native and controlled-provider agent/mixed happy paths. Broader
deadline and failure combinations remain open. The auth follow-up was [installed at schema64/mvp74](shuttle-native-pass-payment-primary-qa-2026-10-20.md); the current primary is mvp78 after its separately accepted [installation/rollback](shuttle-massage-primary-qa-2026-10-20.md).

The [display-name gate](shuttle-profile-four-path-runtime-qa-2026-10-20.md) now
accepts its separate four paths: shared expectations, actual forms, both handoffs
and the original question/run across a 0.875-second graceful stop. This leaves
legal registration identity independent.

## First slice acceptance procedure

Use fresh synthetic participants and a configured synthetic event on one owned,
limited mock-Telegram/real-IAM/Loom stand. Fixture setup may configure event rules
and authorities. It must not insert participants' registrations, complete their
requirements or assign their passes in place of the workflow being accepted.

1. Open registration, choose event/role, receive required-field expectation.
2. Fill required fields in the form with Loom unavailable. Return to a fresh
   role choice, register once, and read the waiting-list status without Loom.
3. Exercise an agent-only user and both continuation directions through the
   same durable fields/expectation/registration. Observe actual tool calls;
   direct test-client mutations are component evidence only.
4. Restart the bot gracefully between pending fields and explicit continuation.
   Replay the accepted role callback, reject stale/foreign choices and verify
   one registration with no unexpected pass or payment.
5. Verify the next reactive agent request sees the owner's manual changes.
   Native paths must issue no Loom request; no proactive agent turn is expected.

A controlled Loom provider proves orchestration and tool contracts. Report that
separately from unattended model comprehension. Actual Telegram smoke acceptance
remains a separate gate, using only the authorized account and test-bot chat.

Archive receipts, logs and the synthetic database before removing the exact owned
stand. Do not alter the primary stand, production or the Loom developer's stand.
Do not expand payment, refund or cancellation policies during this gate.

## Joined native gate and remaining combinations

The [native joined gate](shuttle-joined-native-runtime-qa-2026-10-20.md) now passes
on a fresh owned stand with Loom stopped during the native flow. The procedure
below describes that accepted happy path. The separate joined agent gate also
passes; both whole mixed happy paths now pass separately on candidate mvp75. Event configuration and IAM authorities may be
fixtures. Participant details, registrations, invitation/acceptance, pair and both
passes must result from actual commands/forms, rather than setup writes.

1. Two participants supply required legal fields and explicitly choose their
   respective registration roles. Retain their waiting-list receipts.
2. Invite and accept through the actual pair interface. Read back the original
   invitation, exact partner/role consent and current pair membership.
3. An authorized administrator prepares, reviews and confirms both pass assignments
   through the real native workspace. Participant reads must show those passes
   and the original frozen price/deposit/deadline terms.
4. Submit each participant's deposit and top-up as pending evidence; an authorized
   ambassador reviews them. Confirmed balances change only after review.
5. Read the same final registrations, pair and paid passes from participant
   commands/forms. Repeat a known operation or recover a lost response with its
   original key; no second pair, pass or credited payment may result.

The [joined agent gate](shuttle-joined-agent-runtime-qa-2026-10-20.md) now accepts
the same complete chain through actual Loom tools and free-text consent, including
the original assignment approval across a measured 0.530-second signal-to-exit.
The [whole mixed gate](shuttle-joined-mixed-runtime-qa-2026-10-20.md) now passes
both continuation directions. The native terms fix now has separate mvp77
component acceptance and [isolated binary compatibility acceptance](shuttle-pair-terms-binary-runtime-qa-2026-10-20.md).
Primary candidate installation/rollback now passes separately. The [native initial-deadline and late-decision gate](shuttle-pass-deadline-runtime-qa-2026-10-20.md)
now also passes on mvp77: two retained passes, three proofs, two confirmations and
one rejection, with60.00 BYN credited and no Loom runs. A real45-second deadline
is crossed after graceful restart; the original early file is submitted/reviewed
later. A separate [agent sales-closing gate](shuttle-pass-agent-closing-runtime-qa-2026-10-20.md)
now passes on schema64/mvp78 from an explicitly assigned pass: original early
proof/caption across Loom downtime and normal bot restart, actual sales cap,
30.00-BYN timely confirmation after closing and70.00-BYN late top-up after explicit
ambassador reason/consent. Four completed runs, six notices and no duplicate
financial effects are verified. Both temporary stands are archived/removed; the
first fixture setup failure is retained. Technical approval JSON remains a
presentation follow-up. Broader timing and failure combinations stay open.
Continue remaining orders/meals/massage and hardening requirements. Keep controlled-provider and real Telegram evidence separate.
Do not expand payment, expiry, refund or cancellation policy during this gate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
