# Massage pending discovery source checkpoint

Date: 2026-10-17. Source schema46 / manifest `mvp-49`.
The owned runtime remains schema45 / `mvp-44`; this source is not installed.
This extends [canonical proposal adoption](shuttle-massage-adoption-qa-2026-10-17.md).

Agent `zinusya_massage.pending` and native `GET /app/massage/{event}/pending`
list active own proposals, including proposals created by the other interface.
Each item carries its original ID, event, kind, source, creation/expiry times and
one typed booking or cancellation argument object. Creation source and timestamp
are stored once; adoption and replay do not overwrite them. Equal arguments do
not merge independent proposals. Consumed and expired choices are excluded.
The undeployed migration46 adds these fields and a partial owner pending index;
SQLc-generated models and queries were regenerated reproducibly.

Current owner/deployment/session authorization is required. Reading informational
pending choices does not require a current pass or enabled massage module. A
pending-only event remains in the native event picker until consumption/expiry.
New booking adoption/apply still rechecks current entitlement and exact terms.
The list grants no consent, capacity hold, reservation or payment status.

Pages use ascending UUID cursors, at most20 records and the existing60KiB encoded
massage payload budget. Follow next_after with after until absent. Native reads
use no-store and recheck the session before delivery. No read or preparation
starts proactive agent work. This component supplies discovery APIs; the owner
form pending picker and reactive prompt context are not yet connected.

## Verification

The bounded Linux Go1.27.2/PostgreSQL17 race gate passed all19 selected roots
(27 passing test/subtest events, zero failures/skips). It includes three new
pending scenarios and the neighboring adoption, native session and agent API
scenarios. After formatting, an internal page-limit guard and stronger one-record
pagination coverage, the focused manifest plus three pending roots passed again.
Both runs froze973 inputs. Configured API/database/integration lint reports zero
issues. Independent correctness and code-quality reviews were clean; a fresh
review after the fixes was also clean. The index remains empty.

Coverage proves both discovery directions, original provenance/expiry after
adoption, separate equal-term proposals, book/cancel typed arguments, consumed
and expired exclusion, owner-scoped reads, pending-only event discovery after
pass/module loss, retained denial of new booking, bounded complete pagination,
invalid page size and logged-out native rejection. Peers are mocks. No actual
Loom adapter/model or real Telegram mixed flow is established by these tests.

Both attempts' temporary containers and private volumes were removed. No runner
was OOM-killed, and the ten main Compose services retained IDs, images, start
times and restart counts. Go was limited to2GiB and PostgreSQL512MiB with swap
disabled. Ignored evidence is in `.cache/shuttle-work/massage-pending-v1-qa/`
and `v2-qa/`. No production deployment or publication occurred.

Later source progress: [pending discovery](shuttle-massage-pending-qa-2026-10-17.md)
and [reactive context](shuttle-massage-reactive-context-qa-2026-10-17.md) now have
component acceptance. The remaining items below describe this earlier checkpoint;
native pending selection and actual runtime/mixed acceptance remain open.

## Next acceptance

Connect bounded pre-task pending/action context and native pending selection.
Require revalidation and a fresh explicit approval/click of the exact preview;
history and pending metadata are never consent. Then install the candidate on
the owned stand and test actual native, agent and both mixed directions, including
new Loom-generated operations and lost replies/restarts.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
