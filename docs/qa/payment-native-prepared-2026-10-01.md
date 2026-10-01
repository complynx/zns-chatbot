# Prepared payment candidate: native PostgreSQL result

Candidate `0ef011d3ec8f064f45a929e15bcd1b87153fbcdd`, based on integrated
`7ff6659bcc8095b17cd41bb34f678e486470bf00`. Root verified all seven raw source
hashes against the prepared manifest before execution. Session54824 terminal
exit1, package116.483s. Original12 test parents and all three new parents remained
in the exact15 selection; no selector or assertion was removed.

Verdict: **FAIL, not accepted**. Fourteen top-level tests passed. The remaining
parent `TestPaymentUnavailableContextRetiresRevokedSource` and all its eight
locale/queued/refusal subcases failed with `bot_delivery_stale` at the selected
fixture execution. Diagnosis is assigned to the developer; this report does not
infer whether the cause is product behavior or fixture setup. No fresh Code QA271
has been allocated and this source is not merged.

Authenticated native PostgreSQL17.11 on exclusive loopback58451 completed
before/after inventory readbacks. Snapshots are identical: the four original
databases, three unprivileged login roles, absent zns_app and unchanged parent
owner/version. The stand is released after terminal execution. Private owner
credentials are retained outside Git and are not report evidence.

Raw source-local evidence: `qa.local/root-native-prepared/{tests.jsonl,before.json,after.json}`
in the payment worktree. Raw test log SHA256: `05f778c415552c860223bbc5972f38951d457ee98b78bd61ea1653485b5b84d8`.

Earlier12-case PASS on c5 is historical; it does not accept this new candidate.
Functional EN/RU Telegram-like acceptance and the final full baseline remain open.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
