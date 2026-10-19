# Tier pricing QA — 2026-10-14

The owned stand runs schema 18 and manifest zinusya/mvp-14. Only the authorized
current account in @complynx_test_bot was used in Telegram. Other participants
were local IAM/API fixtures; no invented Telegram ID received a real message.
All prices and event data were synthetic. No money was confirmed or transferred.

The broker/Telegram scenario used image
`sha256:d13ed444bfff11cc052aa3818db2bf346712c70aa5dcc15906fa69af9ae74bdf`.
Final image
`sha256:b5b4a24de43a9faf069bdb028d9a530885327c761eaa4f0754ec50b3bcb7740c`
replaces only the schema's literal bound with a named constant. Its declared tier
output and original owner records/receipt were checked again after replacement.

## Passed behavior

Linux race tests cover role-specific paired prices, distributed pair overflow and
date barriers, implied usage from old flat passes, promo rules, current database
time, competing approvals and rejection before either pair pass is written.
Other checks cover owner-specific deposits, policy edits, old flat preparations,
original receipts and restart. The full suite passed with integration time
141.695 seconds. After the native schema correction below, the affected tier,
registration and assignment checks passed again, including the output-contract
reproduction. Final lint reports zero issues. Test processes had bounded memory;
their containers and disposable PostgreSQL were removed after preserving logs.

Independent correctness and quality reviews found one unstable date-test setup.
The test now retries bounded setup with new configuration versions and operation
keys if a busy runner crosses the floor before preparation. It then checks that
the approved early quote is stale after the floor and a fresh approval uses the
new price. Fresh reviews of the resulting source and test changes found no
actionable findings. Review is separate from native acceptance.

Before the stand migration, a validated PostgreSQL custom archive and the exact
previous bot image were retained. Migration 00018 added only tier history storage.
Digests of existing users, tasks, registrations, passes, payment evidence/reviews
and pairs were identical before and after expansion.

Native API setup created a paired-quota event with early price 100.00 BYN and
regular price 200.00 BYN. Each tier had an independent quota per role. Deposit
terms were minimum 30.00 BYN and 25%, whichever is greater. A first solo leader
consumed the early leader place. Confirming the earlier paired proposal then
returned terminal rejection and created no partial pair passes.

A fresh native Loom run read the pair candidate and requested exact approval.
Its description showed leader 200.00 BYN / deposit 50.00 BYN and follower
100.00 BYN / deposit 30.00 BYN, including each tier ID/ordinal. Approval atomically
created two independent owner passes. The corrected run completed successfully.
The event policy was then reordered and a price changed. After bot restart,
both original owner records, deposit thresholds, tier ordinals and assignment
receipt remained identical.

One Russian Telegram request then asked for the current user's pass and balance.
The owner-scoped Loom script read both using the user token. It returned the frozen
100.00 BYN pass, early tier number 1, minimum deposit 30.00 BYN, zero confirmed and
100.00 BYN remaining. The Russian answer appeared in the actual chat after policy
edits and restart. It did not assign another pass or confirm payment.

Separate native API scenarios accepted a distributed pair at 100.00 BYN each when
one early place remained and a valid later tier existed. The next solo participant
received the 200.00 BYN regular tier. A future date-blocked successor rejected the
same boundary shape before creating any pass. These scenarios used authenticated
fixture consent and exact API preparation/confirmation, rather than another Loom
approval run. Their evidence is native-tier-distributed-proof.json.

## Retained failures

The first config fixture omitted promo/blocked_by_date, which HTTP schema requires
explicitly; validation returned 422 before event creation. A stale pre-prune IAM
probe ID then failed token exchange. The fixture continued with a fresh ordinary
IAM user and a local account; no privileged grant or real Telegram identity was
created for that participant.

The first native paired assignment committed successfully, but Zinusya's advertised
output schema omitted the new optional pass.tier property. Loom correctly retained
operation_unknown instead of inventing an outcome or repeating the write. Direct
status proved the original business receipt succeeded. A behavioral test of the
actual receipt against the advertised schema failed on both tier properties.
The shared registration schema was corrected, and the same test passed.

That original failed QA agent cycle was cancelled through the Loom API after its
successful receipt was saved and rechecked. Its uncertain coordinator audit remains;
this report does not claim that the old pinned contract was reconciled. Its passes
were retained and its assignment was never repeated. A separate event and fresh
run established the corrected happy path. This was a Zinusya integration bug, not
a new Loom issue. Initial failure logs and test exits remain available.

## Evidence and boundaries

Ignored native-tier-checkpoint.json preserves the first failed scenario and known
successful receipt. native-tier-fixed-checkpoint.json records the independent
accepted scenario, exact approval, owner snapshots, original receipt, policy edit,
restart and Telegram read task/run. tier-rollout-proof.json records expansion
digests; tier-fixed-rollout-proof.json records the candidate image. Pre-tier backup
metadata and the archive remain under the owned stand directory. Phase logs/exits
are under .cache/shuttle-work; screenshot tier-native-owner-read.png is retained
under .cache/shuttle-live-test.

Loom used the manual Codex mock mailbox on the isolated operation-seal fixture.
Actual IAM, broker authorization, exact approvals, PostgreSQL transactions and
Telegram delivery were exercised. This is deterministic scenario QA, not an
unattended model evaluation. Allocation,
cancellation/refunds, administrative date bypass, interactive zitadeltg linking
and production cutover remain separate gates. Pair fixture consent used the
authenticated API; the Telegram request in this slice was a read-only pass query.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
