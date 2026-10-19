# Structured solo registration

The working tree adds /register event/role navigation and solo waiting-list
requests without Loom. The owned main stand now runs imagefffc16debed6/schema34.
The image-only rollout preserved all65 table hashes, passed a real-IAM owner read,
and restored the same Telegram poller with a fresh successful cycle. Scoped
[native Telegram and real-Loom acceptance](shuttle-structured-registration-qa-2026-10-16.md)
passed for all four entry/continuation paths with a manually driven provider.
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

## Verification and rollout

Architecture and Go-senior discussion identified the canonical receipt-hash trap
and final locked-expiry requirement before implementation. A separate source
review found untranslated saved role values; these were fixed through the shared
role renderer. Strict lint prompted separating event listing and role presentation.
A compile-only attempt during that extraction rejected an omitted nested literal
type; it was fixed without a lint suppression and the failed attempt was retained.
The initial scoped Linux/race run passed69 root tests /148 root-and-subtest pass
events before those changes, with no failures or skips. Its exact Go/PG containers
exited0/noOOM and were removed; evidence remains under ignored
.cache/shuttle-work/structured-register-qa/. The final expanded Linux/race run at
the corrected source passed92 root tests /174 root-and-subtest pass events:
all32 db,11 API and10 task roots plus39 selected registration/assignment/structured/
name integration roots. All eight new structured registration roots passed. No
failures or skips occurred. Source hashes and all ten main-service fingerprints
stayed unchanged. The final Go/PG containers exited0/noOOM and were removed by
verified identity; receipts are under structured-register-final-qa/. Final strict
lint reports zero issues and a fresh independent source review found no actionable
findings. This is scoped verification, not the full integration suite.

Behavioral proof includes command/event/role navigation with Loom unavailable,
restart and duplicate callbacks, canonical receipt status, wrong-message/foreign/
stale-event rejection, recovery of a committed receipt after choice expiry,
opposite-role conflict, recorded selection context and shared agent/API state,
expiry while blocked on the owner row, uncertain prompt delivery denial, Russian
role output, and a pending agent question surviving registration and restart.
The mixed test uses synthetic peers/authenticated API calls; it does not prove
native model interpretation or a real-Loom tool decision.

No new schema, dependency, worker or authorization subsystem is introduced.
Existing binaries cannot process the new callback/checkpoint path: stop ingress
and drain/reconcile accepted tasks and undelivered outbox before rolling back the
binary. Retain existing registration receipts and database data. Native Telegram
and real-IAM/Loom mixed continuation passed the scoped waiting-list acceptance
linked above. Full registration/pass workflow and unattended model acceptance
remain open; the waiting-list slice does not close those gates.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
