# Structured solo registration

The working tree adds /register event/role navigation and solo waiting-list
requests without Loom.
This is a participant entry slice, not full registration/pass parity: administrator
allocation, pass assignment choices, pair entry, payment forms and configured
name/passport prerequisites remain separate work.

## User flow and shared state

/menu and the normal reply keyboard expose /register. It lists open registration
events through the existing owner-scoped ListEventContexts query, which filters
module availability and closing time before its bounded pagination. Closed or
disabled events cannot hide later eligible events. Event titles are readable
inline buttons; no UUID is used as a visible button label. Event links and next
page controls are reads, not consent to register.

Selecting an event presents its name, start time and choice deadline in that
event's IANA timezone, and Leader/Follower buttons. A typed /register plus the
event UUID opens the same presentation. The role button explicitly requests a
waiting-list entry; it does not assign a pass, reserve assigned capacity, charge
money or imply confirmed payment. The existing registerSolo mutation remains the
business owner, including existing same-role reuse and opposite-role conflict.
The response reads current registration state so a later duplicate click does
not display an assigned registration as newly waitlisted.

Name expectations remain independent. A registration choice never consumes free
text or creates a registration-only name requirement. The absence of configurable
name/passport prerequisites in the current domain is a parity gap, not evidence
that the old Python requirements have been migrated.

## Presentation, transaction and recovery

The role presentation persists event ID, event version and an absolute expiry in
its task snapshot/outbox transaction. Its maximum lifetime is two hours and is
bounded by sales closing. A write callback must identify that prior task and its
role button, with the same authenticated owner, conversation, earlier sequence,
completed task and exact delivered Telegram message_id receipt. Missing or unknown
delivery does not authorize a write. Callback UUID or quoted text alone is not
sufficient. Typed registration commands are reads; writes require the bound button
or the existing authenticated agent/API path.

Before writing, the intake task checkpoints the exact verified presentation,
canonical RegistrationChange, actor and stable operation ID derived from
presentation plus role. The transport guard stays outside the canonical request
hash; existing API/agent receipt formats and RegistrationJoinOperation continue
to recognize the same successful joins. An existing exact receipt is read before
new event/expiry checks. Successful retries survive expiry or a later event edit.
A new operation checks event version under the existing event lock and database
time after the user lock. It therefore rejects a choice that expires while waiting
for that lock. Both sales closing and presentation expiry are enforced there.

The normal durable intake, routing watermark and outbox are reused. Structured
intermediate saves do not replace a pending agent task. A repeated callback update
reuses its intake task; distinct updates from the same role button reuse the same
business operation. The opposite role cannot overwrite an established role.

## Agent continuation and limits

Recent manual context now includes bounded /register facts and its selected
registration event/version/expiry on the next genuine user-triggered agent turn.
These records are untrusted history, not fresh consent, a global event pin or
proof of delivery. Agent tools must read current domain state and permissions.
Both interfaces use the same registrations and receipts. Selecting an event by
buttons followed by a meaningful agent request has an event breadcrumb; no agent
run is started by navigation or the role button itself. Context transport alone
is not acceptance of unattended model interpretation.
