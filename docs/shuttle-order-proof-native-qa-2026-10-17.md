# Owned native order proof and review QA — 2026-10-17

## Scope

Owned shuttle-live-test, schema 45 / manifest mvp-44. Real Telegram Web current
account and @complynx_test_bot only; real Zitadel and owned Loom. All events,
orders, amounts and PDF contents are synthetic. No real money or production change.
This checkpoint covers one native happy path and session-expiry recovery. Agent,
mixed continuation, rejection/exhaustion, pending-work restart and combined upstream
outages remain separate acceptance work.

## Verified native path

1. Created a fresh orders event in Europe/Berlin, an assigned unpaid pass and a
   catalog with complete pricing/capacity. The owner was also assigned ambassador
   authority for this event before any proof submission.
2. In the embedded Mini App, saved two soup portions, reviewed both item prices and
   the required service charge, then confirmed the exact order: 25.68 BYN.
   Submission alone reserved no places and confirmed no payment.
3. Uploaded a unique synthetic PDF through Telegram. The existing manual provider
   acknowledged this file without tools or business actions. This was one real Loom
   run with a manually authored answer; it does not prove model interpretation.
4. In the native order proof form, selected and loaded that file, then submitted
   25.68 BYN. UI and database agreed: pending review, two held resource units,
   and one delivered pending notice despite the payer also being the ambassador.
5. Opened the review link from that actual Telegram notice. The form selected the
   correct event/evidence, showed the payer, original order price and event-local
   deadline, and loaded the protected proof. A real-IAM reviewer content read
   returned exactly the uploaded PDF SHA256.
6. A browser download-event observation timed out. The browser tool returned much
   later than its requested timeout; no approval was made during that wait. A later
   preparation attempt encountered an expired Mini App session. Reopening through
   Telegram restored login and retained the exact earlier request as unknown.
   Checking it did not invent success; Retry same request prepared that same request.
7. Inspected the exact saved decision, reloaded the proof in the fresh session, and
   confirmed through the form. UI showed Decision recorded / Payment confirmed.
   Database evidence became confirmed; one ambassador review recorded 2568 minor
   BYN, two units became occupied, and one workspace became completed.
8. Exactly one confirmed notice and one earlier pending notice were delivered to
   the same recipient. There was no additional Loom run or run input for order
   selection, submission, proof claim, preparation or confirmation. The only new
   Loom run was the explicit uploaded-file acknowledgement described above.

## Evidence and limits

Ignored local checkpoint: `.cache/shuttle-live-test/order-notice-real-qa-v1/checkpoint.json`.
It retains fixture request IDs, native admission/review receipts, allocations,
notification receipts, the original file hash and protected-content comparison.
Screenshot: `native-decision-recorded.jpg` in that same directory.

Browser download success is not established. The separate authenticated API byte
comparison proves protected content delivery, not browser download UX. No duplicate
upload, replacement order, replacement review or automatic payment was used to
work around the observation timeout. The retained unknown request was reconciled
and retried explicitly with its existing operation key.

This checkpoint does not prove unattended agent behavior or production readiness.
Publication remains on hold; the other three prepared fixtures are available for
agent-only and both mixed paths.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
