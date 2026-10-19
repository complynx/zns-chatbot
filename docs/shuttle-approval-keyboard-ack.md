# Acknowledging a resolved reply keyboard

Real Telegram payment-review QA showed "This choice is no longer active" after a
valid free-text approval. The action completed correctly, but this cleanup text
could suggest that the answer was rejected.

The source fix preserves immediate keyboard removal while the agent continues.
A resolved question or decided approval now says "Response received." or
"Ответ получен." Cleanup still uses its independent durable outbox message and
presentation revision. It does not overwrite inline protocol approval buttons or
rely on another pending message being delivered again after deduplication.

The acknowledgement is selected using the retired keyboard's question/approval
type. A historical decided approval cannot turn a newer unanswered question's
expiry into a success acknowledgement; an answered old question cannot do that to
an expired approval either. Unanswered expiry keeps its existing warning.
No tags, text intent parser, new consent or protocol decision was introduced.

## Verification and retained failures

The final bounded Linux race gate passed44 integration roots /64 pass events and
13 task roots /42 pass events with real disposable PostgreSQL and mock peers.
No failure or skip occurred. All848 frozen inputs and ten main stand services
were unchanged. Strict Go lint passed. The focused set includes native/agent
questions and approvals, protocol callbacks, complete literal approval parameters,
structured menus, payment-review entry, outages/restart and vertical recovery.
The changed behavior is covered by the real question/steering delivery test and
an unanswered-expiry case with a retained old approved action; ru/en text cases
also cover accepted, rejected and mixed retained prompts.

Initial independent reviews found that combining cleanup with an existing
message could overwrite inline approval markup or lose cleanup when that message
was deduplicated. That optimization was removed. Review also found acknowledgement
could accidentally refer to a different retained prompt; it now follows the
retired keyboard type. Fresh corrected-source and final-source reviews found no
remaining actionable findings.

The first runtime gate failed in the new test fixture: it looked up the mock run
by run_id although that map uses creation-operation keys. The corrected fixture
finds the matching run and copies its question under the mutex. The failure,
initial parallel-test lint findings and later line-wrap lint finding remain in
separate local logs. They are not counted as successful tests. The successful
second attempt used a fresh database and its own source snapshot.

Both failed and passing QA containers and temporary volumes are removed. Evidence
is under ignored `.cache/shuttle-work/approval-keyboard-ack-qa` and
`approval-keyboard-ack-v2-qa`; lint outputs are separate. Source reviews and checks
did not stage or commit files.

## Acceptance boundary

This fix is component-verified and installed together with the
[readable payment approval summary](shuttle-payment-approval-summary.md) on the
owned stand. The [real Russian free-text approval path](shuttle-payment-approval-real-qa-2026-10-16.md)
now verifies "Ответ получен." after a decided approval. The separate
[English path](shuttle-payment-approval-en-real-qa-2026-10-16.md) verifies
"Response received." The [natural question expiry](shuttle-dialog-expiry-real-qa-2026-10-16.md) now passes.
Real approval expiry and protocol-button checks remain open. Existing four real payment-review happy paths remain historical proof
for their installed image, not acceptance of this later source fix. Production
readiness, unattended model interpretation and full embedded critical paths are
not claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
