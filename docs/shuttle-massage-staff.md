# Massage specialist access and timetable

## Complete next workflow

A global administrator grants/revokes a known local account's private event-bound
specialist access through the agent or a native administrative form. A specialist
without a pass can choose that event and read the shared timetable or their own
client list through either interface. Booking/cancellation by the participant is
reflected in those same records. Public schedule IDs, passes and event payment
roles alone never establish staff identity. Existing global-admin booking history
remains a separate permission and includes retained cancellations.

Architect and Go senior consultations agreed this boundary before implementation.
The Python timetable is event-wide for specialists (`server.py:674-701` and
`plugins/massage.py:1280-1328`). Its Only mine control filters the UI; it is not an
authorization boundary. The own client list filters specialist ownership
(`massage.py:568-606`). Preserve both capabilities, including bounded client names
and usable contact references, without exporting whole user/profile/pass records.
New staff reads will use event-bound private grants rather than inheriting Python's
unrelated `food.admins` list. New grants require global administration.

Instant booking in Python reserves the specialist's own time with themselves as
client and specialist (`massage.py:1004-1013`); it does not grant arbitrary
third-party booking.
See [instant self reservation](shuttle-massage-instant.md) for its shared operation and recovery contract.
Atomic rescheduling remains a later complete workflow.

Telegram specialist-access approval now displays the canonical human terms
without a redundant machine-argument block.
Empty descriptions retain the existing literal fallback.

## Binding store contract

Migration48 adds separate private bindings and prepared-operation history. One
person may hold several aliases. An event alias remains owned by its original
local user even after revocation. Reactivation may name only that user; another
person needs a new public ID. This avoids silently transferring old booking
ownership when display identifiers are reused. There is no binding backfill.

The Store follows the existing schedule seam: administrator authorization belongs
at the API boundary. Implemented read and write HTTP paths require the current
global role. New effects also require
a non-nil identity revalidation callback. Event locks precede actor locks. The
target is scoped to this bot/org and must have an active linked local identity for
a grant; it is not row-locked after the actor, avoiding cross-event actor/target
lock cycles. Staff reads must independently reject revoked identities.

An exact proposal includes event, alias, user, active state and expected binding,
event and schedule versions. The original operation/proposal/hash cannot change.
Successful receipts are stored atomically with the mutation and replay before
later expiry/version checks. A replay reports historical terms, not current
access: callers must read the current binding after reconciliation.

Removing an alias through schedule replacement atomically deactivates its binding
and advances its version. Ordinary label/hour changes do not revoke it. Readding
an alias does not reactivate access without an explicit grant. A binding
can still be revoked after alias removal, module disabling or target revocation.
Granting requires the enabled module and current schedule alias/version. A revoke
still checks the event and binding versions, but needs no current schedule alias.

Neither bindings nor operation history are physically deleted. Migration down
refuses populated tables. Private bindings do not appear in public schedule JSON.

## Shared staff reads

The agent and Mini App Store entry points share current-authority event discovery,
schedule, timetable/client-list and own-client detail implementations.
Read HTTP
routes, binding writes and manifest methods are implemented in source.
No participant pass is required.
Disabling the massage module retains staff reads; public specialist IDs,
passes and unrelated event roles do not grant staff access.

Event-wide timetable pages include non-cancelled bookings across all specialist
aliases, including retained past records. The own-only list and client detail use
all aliases currently bound to the actor. Every record carries frozen booking
terms and a bounded current client display name. Only own-alias records include a
`tg://user?id=...` contact reference. Other client identity/profile/pass documents,
preparations and private registration data are not exported. Cancelled records
remain in the separate global-administrator history API.

A page selects an explicit window and absolute half-open range of at most 24 hours
for the selected event. Historical staff reads are independent of edited current
event dates. It includes bookings overlapping the visible range, even when
they start earlier. Rows order by start instant then UUID. The continuation cursor
must retain event, window, range and ownership filter; it grants no authority.
Each read rechecks local identity and current grants, plus native session,
generation and expiry. Discovery uses one current-grant statement; subsequent
reads authorize again. Schedule and booking pages are separate bounded responses.
Booking pages contain at most 20 rows and 56 KiB of encoded JSON, leaving room for
the API envelope. A shorter page may still have a continuation cursor. Do not
fetch all pages automatically. These are live pages rather than a snapshot lease;
concurrent changes can require restarting traversal.

Event SHARE precedes actor/session locking. READ COMMITTED deliberately obtains
fresh authorization after a lock wait rather than retaining an earlier snapshot.
Binding, schedule and booking writers use the conflicting event lock. Schedule
reads present current event-local offsets; booking terms retain their historical
timezone, service labels, price and occupied interval. Price is not payment
confirmation. Native reads use one transaction/connection, including session
validation; they do not call agent or participant authorization paths recursively.

## Agent and native read HTTP

The read HTTP milestone introduced manifest `mvp-52` and static module `zinusya_massage_staff` with read-only
`events`, `schedule`, `timetable` and `client` methods under
`/internal/agent/massage_staff/`. Each call authenticates IAM and then current
private Store grants. Public tool discovery is not a specialist access grant.
`own_only` is optional and defaults to false in both manifest and HTTP input;
its cursor counterpart remains explicit so a cursor retains its ownership filter.

Native routes use the same Store projections and recheck the current session:

| Method | Path | Result |
| --- | --- | --- |
| GET | `/app/massage-staff/events` | Current staff events; UUID `after`, limit 1..20 |
| GET | `/app/massage-staff/{event}/schedule` | Current schedule and own aliases |
| POST | `/app/massage-staff/{event}/windows` | Current and retained window versions; body event and optional complete cursor |
| POST | `/app/massage-staff/{event}/timetable` | JSON `MassageStaffQuery`; body event must match route |
| GET | `/app/massage-staff/{event}/clients/{booking}` | Own-alias client detail/contact |

Global administration dynamically discovers four new read methods:
`massage_specialist_events`, `massage_specialist_bindings`,
`massage_specialist_binding` and `massage_specialist_target`. The existing
`massage_schedule` read supplies current public aliases. Binding pages return
event/schedule versions and module state, with at most 20 rows in specialist-ID
order; `next_after` is the next `after` value. Inactive ownership remains visible.
Native equivalents are GET `/app/massage-admin/events`, GET
`/app/massage-admin/{event}/schedule`, GET `/app/massage-admin/{event}/bindings`,
GET `/app/massage-admin/{event}/bindings/{specialist}`, and POST
`/app/massage-admin/target`. All require the current global `zinusya.admin` role,
not a participant pass, specialist grant or event-payment role.

Target lookup takes exactly one known local `user_id` or positive numeric
`telegram_user_id`. It returns only the local UUID, Telegram user ID, bounded
current display name and linked/revoked flags in this bot/organization. It is not
a directory, never creates an account and does not export IAM subject, profile,
credentials or registration documents. Revoked targets remain diagnosable.
Native calls validate that the session still belongs to the IAM-authorized actor
inside the same transaction. GET reads use no-store responses; read-only POSTs
also enforce the configured origin. No agent task is created by these HTTP reads.

The existing binding algorithm now has native session-atomic library wrappers
with the same operation UUID/proposal as the agent path. The operation/event/actor
ordering and original receipt semantics remain intact; booking advisory keys
retain their namespace. This establishes a same-operation bridge only. It is not
acceptance of a fresh Loom operation adopting a native preparation.

## Mixed binding continuation before write activation

Architect and Go senior consultations agreed to keep one canonical preparation/
effect row and add actor-scoped immutable operation aliases pointing directly to
that canonical operation. Initial preparation creates a self-alias; an explicit
adoption under a fresh Loom operation must preserve the original proposal,
arguments, expiry and single effect. Completed lookup must recover the original
receipt even after expiry and later binding changes. Pending discovery must
return canonical proposals once, and reactive context must retain the current
user's admin expectations without an unsolicited agent turn.

The canonical Store and reactive admin context components are implemented and
verified below.
Write HTTP is verified in the later section.
The
owned Loom preparation wire accepts only `prepared` with a future expiry; do not
invent a `completed` preparation status or renew an expired canonical approval
for recovery.
The actor-scoped proposal-status read supplies completed recovery.
Binding write tools and native routes now implement this verified HTTP contract.
Preserve its migration bytes; any
further schema change needs a new migration and the corresponding acceptance.

## Canonical binding continuation Store verification

Schema48 now contains actor-scoped operation aliases pointing directly to one
canonical preparation/effect row. Initial preparation and its self-alias commit
atomically. `AdoptMassageBinding` and `AdoptMiniAppMassageBinding` attach a fresh
operation only to the same actor's pending proposal, with exact arguments,
current event/binding/schedule checks and its original expiry. They never renew
approval or grant access on adoption. An operation cannot switch proposals.

Apply resolves an alias and saves one immutable result on the canonical operation.
Concurrent native/agent application serializes under the same event lock and
returns the same receipt. Existing aliases can replay it after expiry and later
revocation; replay still requires current local identity/session and API IAM
administrator authorization. Creating a fresh alias for a completed or expired
proposal is rejected. Existing-operation prepare keeps the original preparation;
future HTTP handlers must reconcile completed/expired proposals through status
rather than advertise an invalid Loom `prepared` response.

`MassageBindingStatus` takes exactly one operation or proposal selector and returns
`pending`, `completed` or `expired`, original canonical operation/proposal,
arguments, expiry and optional original receipt. Both selectors belong to the
current actor. `PendingMassageBindings` returns canonical proposals once, twenty
per page, with optional event selection. Pass `next_after` as the next proposal
cursor. The actor-scoped cursor remains usable if that proposal completes or
expires; a cursor for another event cannot select an event-filtered page.
These Store methods require current global administrator authorization at the API
boundary; they do not independently verify IAM roles.

## Exact agent and native binding HTTP contract

The binding manifest dynamically advertises three additional global
administrator tools. `set_massage_specialist_binding` is an idempotent write with
an exact-parameters preparation and operation-status path. The two other tools
are reads. Discovery never substitutes for backend IAM authorization.

| Agent path under `/internal/agent/administration/` | Contract |
| --- | --- |
| `set_massage_specialist_binding/prepare` | Protocol1, operation UUID, full change arguments; optional `existing_proposal_id` adopts this actor's pending proposal |
| `set_massage_specialist_binding` | Same operation/idempotency UUID, exact normalized arguments, `confirmation.mode=exact_parameters`, `confirmation.preconditions.proposal_id` |
| `set_massage_specialist_binding/status` | Protocol1 and operation UUID; returns original successful receipt or `not_found` when no completed receipt exists |
| `massage_specialist_pending` | Read envelope; optional `event_id` and `after`; canonical pages of at most 20, follow `next_after` as `after` |
| `massage_specialist_proposal` | Read envelope; exactly one `operation_id` or `proposal_id`; returns pending/completed/expired canonical status and optional original receipt |

A change contains explicit `event_id`, public `specialist_id`, known local
`user_id`, `active`, `expected_version` (zero for absent binding),
`expected_event_version` and `expected_schedule_version`. Optional adoption
selection is preparation metadata, removed from the normalized apply arguments.
The readable EN/RU approval description names the event, alias and account with
Telegram ID. Names are untrusted display data; no operation tags are required in
the user's reply. This changes private access, never bookings or payments.

Agent preparation emits only `prepared` with the original unexpired proposal.
Existing-operation preparation for a completed/expired proposal returns 409 and
points to `massage_specialist_proposal`; it does not create replacement consent,
renew expiry or invent a Loom preparation status. Apply/status return historical
receipts. Read current binding before presenting current access. Pending status
or `not_found` from the effect-status path does not authorize a fresh operation.

Native endpoints use `ZinusyaSession` authentication and the configured origin:

| Method | Path | Contract |
| --- | --- | --- |
| POST | `/app/massage-admin/{event}/bindings/prepare` | Operation UUID and full arguments matching route event; optional `proposal_id` adopts an existing pending proposal; `confirmed` must not be true |
| POST | `/app/massage-admin/{event}/bindings` | Same full arguments, operation UUID, exact `proposal_id`, `confirmed=true` |
| GET | `/app/massage-admin/pending` | Optional `event_id`/`after` query, canonical pages of at most 20 |
| GET | `/app/massage-admin/status/operations/{operation}` | Actor-scoped canonical status through an operation alias |
| GET | `/app/massage-admin/status/proposals/{proposal}` | Actor-scoped canonical status through the proposal ID |

Native JSON rejects omitted/null `active` and `expected_version`; explicit false
and zero are valid. Unknown/trailing JSON is rejected. Native preparation returns
canonical status, including the original result for an already-completed existing
operation; the UI must present that receipt rather than request another effect.
A fresh operation cannot adopt a completed or expired proposal. Recovery reads
and writes recheck the current session even for historical receipts.

Each path requires the current global `zinusya.admin` role and current local
identity/session. Apply rechecks IAM inside the binding transaction using only
identity HTTP/cache calls: no Store lookup or session extraction occurs while
actor/event/session locks are held. Native role loss maps to 403; unavailable IAM
is retryable without a new operation. Token caching follows the existing policy;
the after-lock-wait role-loss test invalidates the actor's token cache explicitly.
The reactive prompt now names the actual pending/proposal/write tools for handoff.

## Binding HTTP component verification

Four new integration roots cover native preparation -> fresh agent adoption/apply,
agent preparation -> fresh native adoption/apply, canonical pending discovery,
original status/replay after revocation, completed preparation reconciliation,
explicit false/zero/confirmation rejection, origin and idempotency failures,
expired consent, malformed selectors, non-admin denial on all new paths and IAM
role loss while apply waits for an event lock.
The complete mixed HTTP path uses
one real PostgreSQL connection and an intake acceptor that fails if invoked; no
agent task or proactive run is created.
Queue workers are not started in this
one-connection fixture.
The API wire-schema root verifies exact
confirmation/status/idempotency paths, optional adoption metadata and required
explicit false/zero fields.

Two failed gates remain preserved.
The fixed
`status/operations` and `status/proposals` namespace has disjoint fixed second
segments.
Textual reviews before that gate did not catch the overlap.
The fixture now builds the
actual HTTP API directly on the limited Store; queue requirements and business
checks were not relaxed.

Final configured
API/task/integration lint reports zero issues.

This is actual HTTP plus real PostgreSQL with mock IAM/Loom/Telegram peers.
It does not accept specialist/admin UI, a real Loom write conversation or real
Telegram mixed handoff.
No production readiness is claimed.

## Specialist read form and retained window discovery

Source manifest `mvp-54` adds the read-only `windows` agent method and native POST
route listed above. Both use the existing event/actor/session lock and current
private grants. A page contains at most 20 current or retained non-cancelled booked
window versions. Identity is `(window_id, starts_at, ends_at)` by timestamp instant;
current labels win matching tuples, otherwise the smallest booking UUID supplies
the label. Each booking still retains its own frozen terms. Follow the complete
`next` cursor within the same event; do not automatically traverse history.
SQL may inspect event-local historical rows for deduplication; response size and
application memory are bounded, not total database work.

The Mini App exposes event and party pages, 24-hour time slices, event-wide bookings,
My clients, and own-client detail/contact. Distinct versions of the same party ID
remain separately selectable by their displayed dates. Removing/shortening a past
party or editing event dates does not hide its retained bookings. Exact own-client
reads also do not depend on current event dates. These changes affect staff reads;
participant booking eligibility and current schedule validation are unchanged.

Dates display in the current event timezone with an explicit UTC offset, including
DST overlaps. Frozen terms/timezone remain unchanged in records and API responses.
Client data are not stored in browser session storage; logout, session expiry,
access denial and stale-response guards clear the private view.

A proposed switch to frozen
booking display timezone was rejected against the explicit event-local display
requirement; the differing-zone UI test covers this decision.
Earlier failed gates
remain preserved: a `_windows.go` filename implicitly excluded the new files on
Linux, then a test compared time representations rather than equal instants.

Loopback browser QA used actual HTML/CSS/JS with mock data.
It verified event
selection, own detail/contact href without following it, foreign contact isolation,
DST offsets, Russian labels/prices, own-client filtering and clearing after grant
revocation/session expiry.
A preview-only bootstrap syntax error was fixed before
that inspection.
This is not real IAM/Loom/Telegram or unattended-model acceptance.
The preview server was stopped and its temporary tab closed.

## Native administrator binding form

The Mini App administrative form now has source component acceptance.
Forward installation passes; complete real-Telegram workflow acceptance remains open.

### Using the source workflow

These procedures describe the installed local workflow; complete real-Telegram
acceptance is pending. The actor needs current
`zinusya.admin` in this bot's organization.

1. Native grant/revoke: open the existing Telegram
   Mini App and sign in. Scroll to Massage specialist access / Доступ специалистов
   массажа; the `#massage-admin` fragment also scrolls to it. Choose Event and
   Specialist. For a new binding, enter one known account UUID or Telegram ID and
   press Find this account. An existing binding locks that account. Choose Grant
   access or Revoke access, press Review access change, inspect event, alias,
   resolved account, action and expiry, then press Confirm this exact change once.
   Read the historical result and the separately refreshed current access.
2. Agent-only: explicitly ask the Telegram bot to grant/revoke specialist access,
   naming the event, specialist ID and known account ID. The agent reads current
   data and prepares `zinusya_administration.set_massage_specialist_binding`.
   Inspect its exact action summary and answer its confirmation request in normal
   text or with the offered answer. The agent must use the original proposal and
   parameters; an expired proposal requires a new explicit choice/consent.
3. Form to agent: stop after Review access change, before native confirmation.
   Send an explicit Telegram request to continue that access change. The agent
   uses `massage_specialist_pending` / `massage_specialist_proposal` and adopts the
   same proposal via `existing_proposal_id`, without extending its expiry. Inspect
   the original preview and confirm through the dialogue. Preparing in the form
   alone does not start an unsolicited agent run.
4. Agent to form: leave the agent's prepared change unconfirmed. Open the Mini App
   under the same account and press Review saved access change for that pending
   choice; use the First/Next unfinished choices controls if needed. The form adopts
   that original proposal and shows the same parameters/expiry. Inspect and confirm
   once. A completed proposal is recovery of the original result, not new consent.

After an uncertain response, use Check earlier request before starting another
change. Retry the same request retains its original operation; page reload recovers
its parameters from authorized status reads and never applies it automatically.
Do not create a replacement because a status is missing. An unknown preparation
without recoverable parameters has check/close controls; an uncertain application
keeps new choices blocked until result reconciliation or authoritative expiry.
Close local preview removes only this browser preview, not the server proposal.

An administrator can page configured events and existing bindings, including
removed aliases; resolve one known account UUID or Telegram ID; preview and confirm
an exact grant/revoke; and continue an actor-scoped pending proposal created by the
agent. Existing alias ownership cannot be reassigned. New grants require current
configuration, enabled module and linked/nonrevoked target. Revocation retains
its backend exceptions: it does not require current schedule-version equality,
a configured alias, enabled module or nonrevoked target; event/binding versions
and immutable account ownership still apply. Original arguments are never silently
updated to newer versions.

A separate confirmation shows event name, specialist ID, target display name,
Telegram ID/account UUID, action and original expiry in the event timezone. A
completed result is historical; current bindings are read separately. Closing a
local preview does not delete its durable server proposal.

Owner-scoped session storage contains only actor, event, operation/proposal IDs
and phase. It does not store the target selection separately or retain Telegram ID,
name, display strings, tokens or original arguments for this module. Reload recovers original arguments through
current-authority operation status, with proposal-status fallback when its ID is
known. Canonical operation ID may differ from the locally adopted operation; the
local key is retained for retries. A missing status does not prove failure. An
unknown preparation without recoverable arguments can be checked again or closed
before application; it is never silently replaced. An uncertain application remains
blocked from new choices until original-result reconciliation or authoritative
expiry. Recovery does not automatically apply an action.

HTTP401 clears the workspace and follows sign-in denial; HTTP403 clears/hides the
administrative workspace while retaining only recovery identifiers. A first
service outage instead shows a safe error/Reload state with mutation controls
disabled. Unreadable recovery storage shows an explicit blocked explanation and
retains identifiers. Late replies cannot restore private data after logout or
account change. These UI controls supplement the existing backend authorization.

Three early failed PII-clearing assertions were
caused by an inaccurate DOM fixture; its `replaceChildren` now clears text as the
browser does.
No product workaround was added for that fixture problem.

This used mock data,
not real IAM/Loom/Telegram.

Native administrator and roleless specialist real-Telegram QA now pass.
Next
perform agent-only administrative writes and both form/agent approval handoffs.
Complete live-controller rollback, full massage parity, unattended-model behavior
and production readiness remain unaccepted.
Production and release publication remain unauthorized.
