# Browser session expiry during payment confirmation

A Mini App payment confirmation validated its browser session before the
financial queries. It could then wait on the payer's user lock until that
session expired. The final authority fence checked local invalidation and IAM
credential expiry, but omitted the browser session deadline. Money could be
credited after expiry; the HTTP handler's later session check only hid the
already committed result behind a 401 response.

The fence now includes the captured browser session expiry. A late expiry
therefore rolls back both the financial effect and the outer operation receipt.
Existing subject invalidation and IAM-expiry checks remain in the same fence.

## Verification

The new HTTP test saves a valid ambassador decision, blocks the payer's user
row and waits until the request is actually blocked by that transaction. It
then waits for the browser session deadline to pass before releasing the lock.
The original API created one payment review despite expiry. The fixed API
returns 403 with no review or completed outer receipt.

The first reproduction asserted the HTTP status before inspecting effects and
failed with 401 instead of 403. That failure is retained. A corrected reproduction
against a frozen copy of the original API checked money first and found one
review where zero was required. The two failures are distinct evidence; the
first alone did not prove a financial effect.

The complete review/workspace Linux race gate passed 27 scenarios / 36 cases.
A final adjacent gate passed 35 scenarios / 57 cases, including both the expiry
case and the existing invalidation case, plus owner pass-payment and order-review
paths. Deduplicating those two repeated cases gives 60 scenarios / 91 cases,
without failures or skips. Production source matches both gates. The first gate
predates only test formatting; the final gate includes that formatting.
The final changed-package lint gate reported zero issues; its original two
formatting findings and fixes are retained. Independent source reviews v1 and
v2 reported no actionable findings.

The tests use actual PostgreSQL with HTTP IAM/Loom/Telegram peers, on our own
isolated fixture. Runners have 3 GiB limits without swap; none was OOM-killed.
No installed main-stand or production image was changed. Actual IAM and live
Telegram acceptance of this source remains separate work.

Local evidence: `.cache/shuttle-work/payment-session-expiry-v1/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
