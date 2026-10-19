# Massage specialist notification worker QA — 2026-10-18

The Telegram worker, queue repair, service-start recovery and fixed metric labels
pass a scoped mock-integration gate: twenty integration roots and two
observability roots, with forty-four passing test events and no failures or
skips. This is source/mock acceptance. The main stand still runs schema48 /
mvp-55. Migration49 and this worker have not been installed there.

The [storage](shuttle-massage-specialist-notice-store-qa-2026-10-18.md) and
[reservation](shuttle-massage-specialist-notice-delivery-qa-2026-10-18.md) gates
remain separate evidence. This stage wires their methods into River. Jobs carry
only notice ID and version. Repair schedules committed pending/retryable intents
with argument uniqueness. Service startup recovers inflight notices under the
exclusive instance lock before workers start. Existing notification recovery
order is preserved in one helper.

## Verified mock behavior

A bound specialist without a participant pass receives a new-booking notice in
English or Russian (including Ukrainian Telegram-language fallback). The text
contains the frozen event, service, specialist, price, booking ID, local start
and treatment-end timestamps with the event timezone. Client names are plain
text, including markup-looking characters and an astral Unicode character.
The current own-booking contact becomes one Telegram `text_link` entity; the
test checks its UTF-16 range and target. The optional Mini App button targets
the existing specialist timetable section. Its click behavior is not accepted
by this gate.

Cancellation has a separate notice and no contact entity/link. Cancellation,
module disablement, account revocation, binding revoke/regrant and preference
mute/re-enable during the IAM wait suppress the stale creation intent. No
send reservation is created for the blocked intent. The cancellation case may
still deliver its distinct cancellation notice.

Temporary mock IAM failure leaves the notice pending without a send attempt.
After restoration, a known Telegram 429 rejection produces a safe retry and
then delivery: two HTTP attempts, one accepted message. These selected tests
also assert that business notifications do not start a Loom run. They do not
prove real Zitadel/Loom downtime behavior.

Lost Telegram responses, a pre-existing inflight attempt at startup and failed
writes of delivered receipts end as `delivery_unknown`. The original attempt
is retained and is not automatically resent across service stop/start. These
are in-process service lifecycle checks with HTTP mock peers; they do not prove
an OS/container restart, abrupt process kill or the one/five-second shutdown
requirements. Administrative resolution of this new notice kind is still open.

Queue and callback-duration metrics recognize the fixed
`zinusya_massage_specialist_notice` kind; component metrics recognize
`massage_specialist_notices`. Existing label bounds remain in place. Tests
check the new series and the bounded series counts.

## Review, limits and evidence

Two fresh correctness reviews and a separate quality/runner review returned no
actionable findings. Reviewers read textual diffs and source context only and
ran no commands or tests. Initial lint found duplicated scheduling structure,
function size/complexity, a repeated literal and a test assertion style issue.
The new worker was simplified, recovery methods grouped without reordering,
and the assertion corrected. Final scoped Linux lint for `internal/task`,
`internal/observability` and `integration` reports zero issues. The final race
run uses the frozen corrected source. The index remains empty.

The disposable PostgreSQL and Go containers have 512 MiB and 2 GiB limits,
respectively, and use pinned images. No OOM occurred. The source freeze and ten
main Compose container identities, images, start times, running states and
restart counts remained unchanged. This is not a main business-row fingerprint
or live deployment gate. The exact private QA containers and their database
volume are removed after acceptance; shared Go caches remain.

Ignored evidence: `.cache/shuttle-work/massage-specialist-notice-worker-v1-qa/`
contains checkpoint, frozen source, expected roots, race logs and both lint
outputs. Prior storage/delivery failures remain preserved in their own gates.

Next: add metadata-only administrative reads and audited retry/dismiss for
`massage_specialist`, preserving captured authorization generations even on
retry. Add shared agent/native preference endpoints and controls, then complete
multi-user mock acceptance and separately reviewed main-stand installation and
real Telegram acceptance. Upcoming-session notices remain a later milestone.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
