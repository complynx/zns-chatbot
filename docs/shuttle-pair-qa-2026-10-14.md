# Native pair registration QA — 2026-10-14

The owned stand ran schema 17 and manifest zinusya/mvp-13. Bot image:
`sha256:abf9dbd22d4f69614594b7a452565090f95e51a237e31d531959e6d425537fea`.
Only the authorized current account in @complynx_test_bot was used in Telegram.
The counterpart was an explicitly authorized local IAM/API fixture administrator.
All event terms, documents and amounts were synthetic. No real money moved.

## Accepted sequence

The counterpart joined as leader and created a directed invitation to the current
user as follower. The invitation arrived in Telegram. One explicit Telegram request
accepted it after a native Loom script read. Acceptance created the paired waitlist
without assigning passes or crediting money.

A separate fixture administrator's native Loom run read pair candidates and asked
for exact approval of both participant versions. No pass existed before approval.
The committed result contained two distinct owner passes, each priced at 123.45 BYN.
Both owners remained assigned with independent balances. The prior unrelated fully
paid pass retained its 123.45 BYN balance.

One fresh PDF was uploaded in Telegram with an explicit event, owner pass and
request to submit only pending evidence for 30.87 BYN. It contains SYNTHETIC QA,
NO REAL PAYMENT and NOT A BANK DOCUMENT labels. Native Loom PDF text extraction
returned its amount and identifiers. The authenticated bot file descriptor matched
the local source SHA-256. The short Loom file reference does not expose that hash.
No money was credited by reading or submitting the file.

The same accepted task survived a provider pause and explicit operator recovery,
without another upload or replacement run. A separate fixture administrator then
confirmed exactly 30.87 BYN through native Loom exact approval. No review existed
before approval. The owner received a Russian Telegram notice with Europe/Minsk
decision time, 30.87 BYN confirmed and 92.58 BYN remaining. The pass remained short
of its full deposit requirement and was not fully paid.

One explicit Telegram request dissolved the pair after reading current membership.
The pair became dissolved and its two active memberships disappeared. Both pass
rows remained exactly equal to their snapshots, including price and original
assignment/payment deadline instants. Both balances, frozen deposit policies,
evidence, review and payment-version rows remained exactly equal. Confirmed money
stayed with its owner: 30.87 BYN confirmed / 92.58 BYN remaining; the counterpart
still had 0.00 BYN confirmed / 123.45 BYN remaining. Dissolved pairs correctly leave
the participant's active-pair list. The Telegram response reported the result.

The bot container restarted in 0.757 seconds as measured around docker restart.
This is one idle-container observation, not a loaded shutdown bound. Original
dissolution status, reconciliation and replay with the same operation key returned
the original receipt. The original assignment receipt also stayed unchanged even
though the relationship had ended. Pass, review, pair-notice, task and outbox row
counts did not increase. Financial rows were checked again after the restart.

## Failures retained and recovery

The manual provider helper originally selected an old completed orphan request
instead of a pending step. After the PDF read it stopped before submitting evidence.
Loom exhausted its two sponsored uncertain attempts and paused the run with
platform_retry_limit. The bot retained the task as awaiting_service. No evidence
existed at that checkpoint. An explicit resume with the original limit immediately
paused again, as the configured lifetime attempt budget was still exhausted.

The owned mock-provider stand temporarily allowed one further sponsored attempt.
Existing audit/exposure records were retained; no inference or business state was
edited in SQL. Explicit API resume of the same run then completed submission. The
normal limit of two was restored and the owned Loom container restarted. This is
an operator action, not new automatic recovery behavior in the bot. Automatic
resume remains restricted to an observed shutdown pause.

Other helper failures included incorrect SQL assumptions, expecting a SHA field in
a short file reference, and expecting a dissolved relationship in the active-pair
list. The last assertion failed after a successful business action. Read-only
verification established the committed result; the Telegram request and write were
not repeated to repair that assertion. Initial failure logs and exit codes remain.

A local Loom introspection secret accidentally appeared in diagnostic output. It
was rotated through the owned Zitadel API, both local runtime sources were updated,
and the owned Loom container restarted. Subsequent authorized native calls passed.
No secret value is included here. Production credentials and permissions were not
changed.

## Evidence and limits

Ignored `.cache/shuttle-live-test/native-pair-checkpoint.json` records fixture IDs,
run/task IDs, exact approvals, original receipts, financial snapshots and restart
assertions. `native-pair-pdf-source.json` records the original document hash/size.
Tool outputs and screenshot `pair-native-dissolve.png` are retained beside it;
phase logs and exit codes are under `.cache/shuttle-work/`.

Real Zitadel, Loom authorization/broker approvals, file-worker PDF extraction,
PostgreSQL business transactions and Telegram delivery were exercised. Loom used
its mock mailbox with manual Codex responses. This is deterministic scenario QA,
not an unattended model or arbitrary media-classification evaluation. The stand
also uses the isolated operation-seal fixture documented separately. Counterpart
Telegram delivery was denied locally by the test guard; no invented counterpart
ID received a real message.

Source behavioral/race checks and lint passed before native QA. This accepts
invitation, consent, atomic assignment, confirmed-partial-money preservation,
participant uncoupling and original receipts after restart. Tier pricing,
deterministic allocation, cancellation/refund policy, all delivery-resolution
paths, interactive zitadeltg linking and production cutover remain separate gates.
Loom PDF rendering issue #4 remains open; embedded-font text extraction does not
establish that the upstream rendering issue is fixed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
