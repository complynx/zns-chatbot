# Administrator assignment with exact confirmation

The `zinusya_administration` module advertises an `assign` write in manifest
revision `mvp-5`. It requires `zinusya.admin` in both the Loom and bot API projects,
an active local account and an explicit administrator request. Existing operator
HTTP assignment remains available for explicit operator calls; it is not used as
a shortcut around the agent confirmation contract.

## Agent flow

1. Read `events` or `event` configuration and `registrations` for the selected
   event through the script module. List limits are 1–20, default 20; follow `next`
   as `after`. UUID paging is not waitlist priority.
2. Call the model tool `module_call` with module `zinusya_administration`, method
   `assign`, and four exact arguments: `event_id`, `registration_id`,
   `expected_event_version`, `expected_version`.
3. Loom prepares the operation and collects exact-parameter human approval.
   Preparation describes the target Telegram account, role, event, exact decimal
   currency price, and payment policy in the event timezone. The policy is a
   configured duration after assignment capped by sales closing; preparation does
   not promise a fixed assignment instant. No payment is recorded.
4. Execution rechecks authorization, the durable proposal, both versions, current
   module/account availability, role capacity, sales closing and proposal expiry.
   Registration, frozen pass snapshot and original operation receipt commit together.

The proposal has a random UUID and is bound to bot, organization, actor, operation
ID and the exact assignment digest. It expires at the earlier of five minutes
from preparation and sales closing. Preparation creates no pass and reserves no
capacity. A second administrator may consume remaining capacity before execution;
the confirmed operation then rejects rather than exceeding capacity. Changed price
or policy changes the event version and requires fresh preparation and approval.

Database time is sampled after recipient locks and capacity checks before assignment.
Waiting on an account-row writer cannot carry an earlier clock past a deadline.
Owner registration and preparation also refresh the clock after recipient locks.
A known committed receipt remains replayable after expiry and application restart;
changed arguments or proposal IDs do not become valid because a receipt exists.

## Wire protocol

All calls require the current delegated user bearer token and repeat the administrator
check. Bodies are limited to 8 KiB; advertised deadline is five seconds.

- `POST /internal/agent/administration/assign/prepare` accepts the ordinary read
  envelope: `protocol_version:1`, `operation_id`, optional `run_id`, `arguments`.
  Returns `status:prepared`, the operation ID, normalized `arguments`,
  `preconditions:{proposal_id}`, human `description` and UTC `expires_at`.
- `POST /internal/agent/administration/assign` additionally requires an
  `idempotency_key` equal to the operation ID and
  `confirmation:{mode:exact_parameters,preconditions:{proposal_id}}`.
  Missing or downgraded confirmation is invalid. Known business rejection returns
  the Loom `rejected` envelope; uncertain storage/commit failure remains an HTTP error.
- `POST /internal/agent/administration/assign/status` accepts
  `protocol_version:1` and `operation_id`. Returns the original prepared assignment's
  `succeeded` result or `not_found`. It excludes other actors and unprepared operator
  receipts. A missing receipt does not prove that a delayed request cannot execute;
  current Loom treats it as unknown. Never replace the operation blindly.

A successful result uses the registration/pass schema in
[solo registration](shuttle-solo-registration.md). The assignment result limit is
16 KiB. The three supporting reads have 64 KiB limits and reuse scoped operator
handlers. Administrative discovery exposes seven methods: the previous diagnostics,
three event/registration reads and the confirmed assignment. Non-admins see none.

## Persistence and rollback

Migration `00007_assignment_preparations.sql` adds immutable preparation records.
They and their original assignment receipts have no automatic deletion. A binary
rollback keeps both tables. Dropping preparation data would erase confirmation and
status binding; an intentional down migration requires a reviewed data export and
reconciliation, not merely a previous executable.

## Verification boundary

Behavioral HTTP/PostgreSQL checks cover preparation without assignment, exact
preparation replay, key/actor/argument/proposal binding, required confirmation,
restart, receipt replay after expiry, stale price/version, capacity consumed after
preparation, owner/admin revocation and permission-filtered discovery.

Independent correctness review found a pre-lock clock crossing proposal expiry.
The HTTP/real-PG lock-wait test reproduced an incorrect successful assignment, then
passed after final clock sampling moved after the recipient lock. A fresh textual
review found no actionable findings in the repair. The quality reviewer found no
practical API/wiring defect. Review used supplied production text because local
reviewer file transport failed; generated sqlc and tests were verified by root.

Final full production-package and PostgreSQL integration race checks passed
(integration 28.631 s), and the linter reported zero issues. The named owned QA
container `zinusya-assignment-qa-fixed-20261013` exited 0 and retains its logs.

Direct real-IAM wire acceptance passed with the fixture administrator and the
current roleless Telegram test account: prepare denial for the roleless account,
preparation without assignment, stable proposal replay, forged confirmation
rejection, successful assignment, filtered seven-method help and event/queue reads.
An actual bot restart then preserved the committed assignment and its status even
when the proposal expired. Evidence is ignored
`.cache/shuttle-live-test/assignment-confirmation-proof.json`. Existing
users/tasks/outbox counts remained 2/8/8. Final app image:
`sha256:de8f8d9c4e5ff8e1f6230184dce84dabcd35d901b9afe4ede8f496fe68a0a676`.
No Telegram message was sent for this direct API acceptance.

Native Loom exact-approval acceptance subsequently passed on 2026-10-13 with the
[isolated upstream working seal fixture](shuttle-loom-seal-fixture.md): real IAM,
side-effect-free preparation, exact parameters/price, approval input, dispatch and
receipt status. The upstream committed release and Telegram UI remain separate
pending gates; the original issue 3 failure is preserved.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
