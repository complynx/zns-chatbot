# Native avatar administrator API — QA, 2026-10-19

Schema59 / manifest mvp68 source is accepted for store/HTTP behavior and bounded
reactive context. This is an API stage, not browser UI or new real-IAM/Loom runtime
acceptance. The primary stays schema49/mvp56, retained candidate image mvp65.

## Final evidence

- 33 root integration tests pass on isolated real PostgreSQL: nine native admin,
 five adoption, four proposal reads,11 previous resolution and four historical /
 finalization migration/rollback roots. The handoff root includes both native-to-
 agent and agent-to-native HTTP paths, followed by exact replay and one delivery
 version change. These HTTP paths use mock IAM and Loom peers, not actual Loom.
- 69 root Linux Go1.27.2 tests pass: API19, task16 and identity34, including the new
 local invalidation-fence behavior. Scoped golangci-lint reports0 issues. sqlc
 regeneration leaves generated output unchanged. Current source hashes and root
 outcomes are retained in the ignored source gate.
- A blocked native HTTP confirmation with pool_max_conns=1 loses its local IAM
 authorization generation before the row lock is released: HTTP403, old delivery
 intact and no completed resolution receipt. There is no IAM I/O in the transaction.
- Session expiry while waiting also rolls confirmation back. Expiry of an existing
 proposal while waiting prevents native preparation. Key generation, wrong-session
 owner, local logout, current role loss, changed arguments, cross-admin ownership
 and missing explicit confirmation are checked on critical paths.
- Native history stays at five prior locators, deduplicates replay, excludes later
 actions by task cutoff and starts no proactive run. A subsequent explicit mock
 agent task receives the proposal locator and continuation instructions; role loss
 withholds them. No bearer is exposed.
- Migration checks preserve history, refuse nonempty action rollback and fail fast
 on active writers. Database dumps/logs from all three PostgreSQL runs are archived
 before exact-owned containers/volumes are removed. The SDK container is removed.
 No OOM occurred. The primary11-container epoch and domain preservation checks pass.
- The index is unchanged; no commit, push, primary install or production deployment.

## Review and retained failures

The first run ended after four recorded root outcomes: two passes, a rollback
failure and a fixture panic. The new action table's foreign keys could make Down
wait for a referenced user table before a later NOWAIT guard. Explicit NOWAIT
locks now cover those parents. The native fixture also tried to bootstrap against
an HTTP server closed by its parent avatar fixture; it now reopens only that HTTP
surface. Raw failure evidence and the database dump are retained.

The second run passed30 of31 roots. Its foreign-admin probe still held a cached
ordinary-user IAM credential and correctly received403, while the test expected
an administrator's actor-scoped404. The probe now invalidates that old credential
before testing the other administrator. The final run passes all33 roots.

An independent correctness review identified the in-flight invalidation window.
The fix reuses existing client invalidation generations as a pure local fence,
plus credential expiry, and adds the blocked single-connection HTTP test above.
It does not add network calls under database locks. A fresh correctness review
and a separate fresh quality review report no actionable findings. Earlier failed
runs and review findings are not relabelled as successful runtime acceptance.

## Next acceptance

Build the native operator UI, including current delivery inspection, original
proposal recovery, exact approval, response-loss recovery and both languages.
Then rebuild an owned candidate and exercise real IAM/Loom/browser handoffs and
restart behavior with the same canonical proposal/result. Previous real IAM/Loom
repair evidence predates adoption/native support. Actual Telegram WebView and
unattended model acceptance remain separate. See the current
[native contract](shuttle-avatar-native-administration.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
