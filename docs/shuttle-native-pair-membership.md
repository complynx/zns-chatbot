# Native current pairs and participant leave

Four functional
paths pass on an isolated real-IAM/Loom stand.
The actual Telegram form displays
four current pairs with the corrected checkboxes, and its new session was logged
out.
That Telegram gate is read-only: it does not exercise a real Telegram pair
write.
Full registration/pass parity, unattended model understanding and
production readiness remain separate requirements.

## API and business records

- `GET /app/pairs/my?event_id=&after=&limit=` lists the current owner's waitlisted
  or assigned pairs. Pagination and participant ownership match the agent's
  existing `zinusya_pairs.my` read. Historical invitation receipts are separate.
- `POST /app/pairs/dissolve` accepts `operation_id` and `arguments` containing
  `event_id`, `pair_id` and `expected_version`.
- `GET /app/pairs/operations/{operation}?kind=dissolve` recovers the original
  dissolution receipt. A receipt describes that operation, not current membership.

The write uses the existing agent domain operation and receipt namespace. It
checks the current Mini App owner/session after acquiring the event and owner
locks, before receipt replay or mutation. A session revoked while waiting for an
event lock cannot execute the write. Retry uses the original operation key and
arguments. A changed request conflicts. A different participant cannot read the
actor's receipt or act using that identity.

Existing participant policy is unchanged: either participant can end membership,
including after sales close or module disablement. Administrator rights do not
substitute for participant ownership. Dissolution removes active membership and
advances registration versions. It does not cancel passes, change price/deposit
terms, confirm payments or create a refund.

The next user-triggered agent request receives up to five own invitation and
membership operation facts. A dissolution fact contains its pair ID and original
state. Facts are marked untrusted history, not instructions or consent. No
proactive agent run is started by native entry.

## Form

The existing pair form shows current pairs separately from invitation history.
Each current row shows its event, state and event-local time. Leaving requires an
explicit checkbox and button click. The form re-reads the current pair/version
before sending; a changed pair requires renewed review. Text is rendered inertly.
Both English and Russian labels are provided.

The owner-bound recovery record contains the exact operation key, action and
arguments before dispatch. An uncertain outcome blocks replacement actions.
Reload recovers the original receipt; an uncommitted request can retry the same
body. A rejected request can be checked and closed under the existing rules.
Logout clears owner data and ignores late responses. Refresh reflects an agent's
completed dissolution without rewriting accepted invitation history.
