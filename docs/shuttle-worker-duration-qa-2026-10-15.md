# Worker callback duration acceptance, 2026-10-15

This bounded observability slice is accepted on the owned `shuttle-live-test`
stand. It adds one OTel histogram to three existing River callbacks, preserving
their returned errors and existing operation counters. No migration or dependency
was added. See the [metric contract](shuttle-worker-duration.md).

## Source verification

Architecture and Go consultations agreed on ordinary post-call recording and
return-only outcomes before implementation. Two fresh independent source
reviewers found no actionable correctness or quality defects. A separate native
helper review checked no-op safety and exact cleanup before execution.

All 225 enumerated integration scenarios and the other explicit Go packages
passed under the race detector. Enumeration, package checks and fifteen bounded
integration batches produced 17 terminal zero exits without OOM. Strict lint
reported zero issues. All 309 frozen source hashes stayed unchanged through the
regression gate, candidate build and native checks. The index remained empty.

Focused checks prove that a blocked Telegram request contributes no duration
observation before callback return and its wait is included afterward. Actual
PostgreSQL/River execution covers ordinary completion, IAM-unavailable snooze,
corrupt-checkpoint error with lease cleanup, payment delivery, persisted unknown
payment delivery and pair delivery. Unit checks cover fixed-label folding,
secret canaries, units/buckets, cancelled contexts and explicit/wrapped/joined
River return classifications. Explicit cancellation is a classification test;
no native production callback was forced to invent that outcome.

The first focused integration run failed because the fixture expected a Loom
GET although its default run completed at creation, and because direct intake
with no enqueue callback created no River job. Tests were corrected to gate the
actual Telegram send and enqueue normally with an isolated corrupt-checkpoint
trigger. Original failure logs remain retained. Product behavior was unchanged
by those fixture corrections. A native helper column typo was corrected during
review before any execution.

## Owned native proof

The accepted candidate image is
`sha256:ad8ffca296a0a17f7beb3979e463a919a76709244b7b00056b0c1b3844e66a24`.
It uses the accepted revocation image as base. Schema remains 30 and manifest
remains `mvp-29`. Mini App origin and current settings are preserved. A private
database dump was saved and its archive listing checked before rollout; this
slice did not rehearse a full database restore or previous-binary rollback.

Exactly three reviewed River jobs exercised the actual production wrappers:
one newly created closed task in a chat-0 conversation, and two absent
notification IDs. The task returns before authorization or delivery; missing
notifications return before recipient checks or sending. Each job completed on
its first callback and produced one `returned` sample with a positive duration
sum. These are deliberate no-op paths, not real IAM/Loom requests or successful
Telegram deliveries. Delivery/error/snooze proof belongs to the source tests
above; native error, snooze and cancellation were not exercised here.

Private `/metrics` exposed only fixed callback labels. Generated fixture IDs and
markers were absent. Public `/metrics` returned 404. Only the three exact River
rows, synthetic task and conversation were deleted. Hashes of 28 selected
business/settings/audit/proposal tables matched their pre-rollout values after
cleanup. A subsequent bot restart removed the process-local histogram samples.
No Telegram message was sent and no money, registration, identity or permission
was changed. The owned stand retained nine bounded services without OOM; all
temporary Go runners, the separate QA PostgreSQL container and its exact volume
were removed after verification.

Private scripts, source freeze, batch plan, terminal states and native checkpoint
are retained under ignored `.cache/shuttle-work` and `.cache/shuttle-live-test`.
This is local stand acceptance, not production rollout, unattended attachment
classification, a latency SLO or completion of all roadmap stages. Interactive
CA/zitadeltg and recorded business-policy decisions remain separate gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
