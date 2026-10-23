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
