# Deposit policy and frozen pass terms

Event configuration accepts optional `document.deposit` with explicit
`minimum_minor` and `minimum_bps`. Values use the registration currency/scale;
10,000 basis points means 100 percent. Missing/null fields inside the policy,
unknown fields, negative values, shares above 10,000, and amounts above the event
price are rejected. Policy requires registration configuration. Omitting the
policy disables partial-payment terms for new assignments; existing snapshots stay.

The required deposit is `max(minimum_minor, ceil(price_minor * minimum_bps / 10000))`.
Both configured minima must hold. Share calculation uses quotient/remainder to
avoid overflow and remains exact up to the supported integer limit 2^53 - 1.
For 123.45 BYN, a minimum amount of 30.00 BYN and share of 25 percent require
30.87 BYN. No currency conversion is performed.

Assignment atomically freezes the configured amount/share, effective required
amount and final top-up deadline in a separate pass child record. The initial
deadline remains the assigned payment window capped at sales closing; the final
deadline is the sales-closing instant captured at assignment. UTC instants are
stored and displayed in the event IANA zone. Administrator preparation discloses
the exact threshold and local final deadline before approval.

Event editing still uses the existing version, operation ID and original receipt
contract. Changing event price, deposit policy or closing does not rewrite existing
pass terms. Assignment replay returns the original result. Existing passes and
old receipts without deposit data remain readable; policy is not inferred or
retroactively attached to them. Deliberate policy removal affects future assignment.

Migration 00009 adds event and pass child tables. Apply it before the new binary.
Do not run old writers for new assignments after enabling deposit policy: they do
not understand this policy. A deliberate rollback requires preserving the child
records; it does not make old assignment logic enforce partial-payment terms.

Introduced in `mvp-7`, current manifest revision `mvp-8` includes the optional event policy in context summaries
and the frozen snapshot in registration/pass results. The authenticated operator
configuration API, owner registration reads, agent tools and exact administrator
assignment all share these records.

This slice defines payment terms. Payment evidence, ambassador confirmation,
confirmed ledger, balances and payment-state transitions are not implemented yet.
No payment is credited, and expiry never cancels an assigned pass or forfeits money.
Those are the next roadmap slice.

Acceptance on 2026-10-13: independent correctness and quality reviews, then a fresh
final review found no actionable findings. Full Linux/PostgreSQL race suite passed
(integration 32.586 seconds), focused final checks passed (3.660 seconds), and lint
reported zero issues. Behavioral cases cover share/amount precedence, upward
rounding, large exact integers, invalid configuration, authorization, frozen price
and policy, later assignment using edited terms, restart and original receipt replay.

Owned native stand: real Zitadel acceptance passed, including actual bot restart
and policy/price edits after assignment. Real Loom exact approval and schema-validated
broker assignment passed on the isolated seal fixture; preparation included the
30.87 BYN threshold. Provider responses were manual scenario mailbox data.
No new Telegram messages were sent; Telegram UI acceptance remains pending.
Ignored evidence: `deposit-policy-proof.json`, `native-deposit-assignment-proof.json`.
Candidate image: `sha256:1ca6946e1b08142c5aabd6de4beeba3ba1eee87d19e31de4f4f292bfca221a85`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
