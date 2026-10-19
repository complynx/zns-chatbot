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
| Profile display name as a separate workflow | Not accepted in this gate | Not accepted in this gate | Not accepted in this gate | Not accepted in this gate |
| Required legal fields through solo leader waiting-list registration | Passed, Loom stopped | Passed, controlled provider | Passed, controlled provider | Passed across restart, controlled provider |
| Pair joining followed by pass assignment | Not accepted as a whole flow | Not accepted as a whole flow | Not accepted as a whole flow | Not accepted as a whole flow |
| Partial pass payment, review and top-up | Not accepted as a whole flow | Not accepted as a whole flow | Not accepted as a whole flow | Not accepted as a whole flow |

These cells describe the new complete-workflow gate, not a claim that earlier
component tests or narrower runtime checks failed. The [native runtime gate](shuttle-registration-native-runtime-qa-2026-10-19.md)
accepts required legal fields through solo waiting-list registration with Loom
stopped. The [agent/mixed runtime gate](shuttle-registration-mixed-runtime-qa-2026-10-19.md)
now accepts the other three paths with real Loom and a controlled provider.
This covers one leader happy path per interface combination, not every role,
profile field, authority or payment variation. Assignment/payment remain separate
slices after their actual user and authority entry points are traced.

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

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
