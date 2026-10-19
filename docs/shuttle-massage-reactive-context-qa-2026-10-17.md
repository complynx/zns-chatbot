# Massage reactive context source checkpoint

Date: 2026-10-17. Source schema46 / manifest `mvp-49`.
Owned runtime remains schema45 / `mvp-44`; this source is not installed.
This extends [pending discovery](shuttle-massage-pending-qa-2026-10-17.md).

The existing user-triggered profile prompt now includes at most five active
massage proposals and five original operation receipts, newest first in each
list with deterministic ID ordering for equal timestamps. Records must have
been created strictly before this owner's accepted task. Pending records must
also remain unexpired and unconsumed when read. Both queries share the existing
owner-authorized repeatable-read transaction and verify task owner plus event
bot/organization. Missing or foreign tasks produce no context; a wrong current
owner/deployment binding is denied.

Pending items reuse the discovery API's typed business arguments and original
creation provenance. Compact receipt items include operation, proposal, booking,
event, kind, source, original state and time; they omit full snapshots. Historical
context remains readable after pass/module loss. It neither restores booking
entitlement nor authorizes an effect. Preparation and apply still recheck access.

The prompt marks the projection as untrusted metadata, not instructions, consent
or a current request. It directs the agent to current pending/get/operation reads,
exact existing-proposal adoption and fresh human approval. It forbids silently
creating replacement operations from history or treating a proposal as a hold or
payment. The same existing prompt path supplies new runs and steering text for an
active dialogue. Native reads, preparation and apply do not start an agent turn.

No new journal, service, dependency, migration or public API was introduced.
SQLc supplies the two bounded projections. The typed pending conversion is shared
with the existing discovery page; its response contract remains unchanged.

## Verification and retained failure

The first frozen Linux/PostgreSQL race run stopped on a new test-fixture panic:
the massage fixture intentionally stops the worker, but the new acceptance helper
called that stopped service. The failure is preserved. The tests now start the
worker explicitly; production source did not change for this repair.

The final bounded Linux Go1.27.2/PostgreSQL17 race run passes all10 selected roots
with zero failures/skips: two new reactive-context scenarios, three pending
scenarios, three native massage command scenarios, the neighboring orders
continuation case and the manifest contract. All976 frozen inputs remained
unchanged. Configured database/task/integration lint reports zero issues. Independent
correctness and quality reviews were clean, as was a fresh final review.

The new tests exercise a native saved proposal in a new Loom request, native
booking while the agent awaits a reply, a separate pending choice, bot restart
and subsequent steering containing that choice and the native receipt. They
assert that native actions do not start another run or send unsolicited steering.
Other coverage checks the five-item caps, newest-first order, pending/receipt
cutoffs, historical context after module loss, unknown/foreign task isolation,
wrong bot/organization denial, original receipt state and absence of session
credentials in prompts. IAM/Loom/Telegram are mocks; this is not actual model
adoption, real Loom adapter execution or real Telegram mixed acceptance.

Both attempts' temporary containers and private volumes were removed after their
terminal results. No runner was OOM-killed; the ten main Compose services retained
IDs, images, start times and restart counts. Go was limited to2GiB, PostgreSQL512MiB,
with swap disabled. Ignored evidence: `.cache/shuttle-work/massage-context-v1-qa/`
and `v2-qa/`. The index remains empty. No production deployment or publication.

## Remaining work

Connect native pending selection to revalidation of the same proposal and a new
explicit confirmation click. Then install the combined massage candidate on the
owned stand and check actual native, agent and both mixed Telegram workflows,
including new Loom-generated operations, expiry, lost replies and bot restarts.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
