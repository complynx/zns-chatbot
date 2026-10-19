# Massage proposal adoption source checkpoint

Date: 2026-10-17. Source schema46 / manifest `mvp-48`.
The owned runtime remains schema45 / `mvp-44`; this source is not installed.
This extends the [native command component](shuttle-massage-command-qa-2026-10-17.md).

Agent book/cancel preparation accepts optional `existing_proposal_id` alongside
required business arguments. Native `/prepare` accepts optional `proposal_id`.
Omitting the selector prepares a new proposal; explicitly supplying null, zero or
an invalid identifier rejects the request. Native decoding returns HTTP400;
Huma decoding errors on the agent route return HTTP422.

Adoption verifies owner, event, operation kind and exact normalized arguments.
It rejects consumed or expired proposals, then rechecks current terms and access.
New booking requires an enabled module and assigned pass. Historical owner
cancellation remains available after module/pass loss. Adoption creates no new
proposal, receipt, capacity hold or expiry. It returns the original proposal and
expiry. Agent responses echo the incoming new Loom operation ID and return base
arguments without the selector. Native preparation rejects `confirmed:true`;
a separate explicit confirmation is still required for apply.

The existing canonical apply path handles competing interfaces. If the native
operation consumes the proposal while an agent approval is pending, agent apply
returns the original result rather than another booking. Original receipts remain
historical after later cancellation. Consumed proposals are read through history
and operation tools, not prepared again.

## Verification and retained failures

Architect and senior Go consultations agreed this contract. The owned frozen
Loom fc6c574 source accepts normalized preparation arguments and opaque returned
preconditions while requiring the incoming operation ID. No Loom source changed.
The integration helper follows that wire contract; it does not execute the real
Loom adapter or establish actual model adoption.

The initial 15-root Linux/PostgreSQL race gate passed, but independent review
found that explicit native zero selectors could fall back to new preparation.
The expanded second run caught the same fallback for agent null input despite a
non-nullable schema tag. A shared typed decoder now rejects supplied null/zero.
The third broad run passed 14 roots and failed two assertions that expected400
instead of Huma's422 decoding response. Only these two expected status codes
changed afterward. The final focused race run passes both remaining roots.
The earlier failed runs remain failed in retained evidence.

Combined coverage is 16 distinct roots with zero skips: original proposal/expiry,
new operation normalization, both handoff directions, concurrent canonical apply,
foreign ownership, changed arguments, expiry, prepare consent rejection, explicit
null/zero on both book/cancel interfaces, no replacement proposals, historical
cancellation, lost access, session replay and rollback. The three latest runs
froze970 inputs; the broad-to-focused difference is solely the test assertions.
Configured API/database/integration lint reports zero issues. Fresh final
independent review reports no actionable findings. The index remains empty.

All four attempts' temporary containers and private volumes were removed after
terminal results. No runner was OOM-killed; the ten main Compose services retained
IDs, images, start times and restart counts. Go was limited to2GiB, PostgreSQL512MiB,
with swap disabled. Ignored evidence: `.cache/shuttle-work/massage-adoption-v1-qa/`
through `v4-qa/`. This is source component acceptance.

Later source progress: [pending discovery](shuttle-massage-pending-qa-2026-10-17.md)
and [reactive context](shuttle-massage-reactive-context-qa-2026-10-17.md) now have
component acceptance. The remaining items below describe this earlier checkpoint;
native pending selection and actual runtime/mixed acceptance remain open.

## Remaining acceptance

Pending discovery, real creation timestamp/provenance, compact reactive context
and native pending selection remain open. Real Loom-generated operations and
fresh conversational approvals, real Telegram native/agent/mixed workflows,
owned-stand rollout and unattended model behavior are not accepted by this gate.
No production deployment or publication occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
