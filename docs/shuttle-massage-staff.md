# Massage specialist access and timetable

Updated: 2026-10-18. The primary owned runtime is schema49 / `mvp-56` after
[specialist notification installation](shuttle-massage-specialist-preferences-runtime-qa-2026-10-18.md)
and the [reboot resume checkpoint](shuttle-reboot-resume-qa-2026-10-18.md).
The binding store was introduced in migration48;
[its forward installation passed](shuttle-massage-staff-install-qa-2026-10-18.md).
Native staff reads and the administrative agent/form happy paths now pass
[real Telegram QA](shuttle-massage-staff-mixed-real-qa-2026-10-18.md). Complete
parity, fault/outage coverage and live-controller rollback acceptance remain open. Separate mock-Telegram QA stands use schema48 / `mvp-54` with real
IAM and Loom. Scoped [native HTTP and actual Loom staff acceptance](shuttle-isolated-domain-qa-2026-10-18.md)
passes there, including current grant/revoke and native booking/cancellation
visibility. Real native staff reads and administrative happy-path handoffs now
pass their linked scoped gates; complete parity remains open.
The historical [catalog contract image activation](shuttle-catalog-contract-install-qa-2026-10-18.md)
changes catalog guidance only; staff contracts are unchanged.
Exact binding write/status/pending HTTP has component acceptance below.
Both specialist read and administrative binding forms have source component
acceptance. Owned forward installation and scoped real native/agent/mixed
staff workflows pass. This is not complete massage parity.

[Real Telegram native staff QA](shuttle-massage-staff-native-real-qa-2026-10-18.md)
now passes administrator grant/revoke/restore without an agent run, no-pass
specialist timetable/own-client/contact reads after removing temporary IAM admin
roles, one actual Loom read/reply, and same-container bot restart. Synthetic
fixture bookings and manual provider responses bound this acceptance. Temporary
admin grants were removed. The subsequent [real approval handoff gate](shuttle-massage-staff-mixed-real-qa-2026-10-18.md)
also passes an agent-only revocation and both form/agent directions: three canonical
effects, original proposal/expiry preserved, six tasks completed and nine expected
messages delivered. Its final fixture binding is revoked at version6, temporary
IAM grants are again removed and no task is pending. Complete live-controller
rollback, unattended model behavior and production acceptance remain open.

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
third-party booking. Specialist notification preferences/delivery now have their
linked scoped runtime acceptance. [Instant self reservation](shuttle-massage-instant.md)
has domain/store, HTTP and isolated shipped-form acceptance, including lost-reply
recovery and both direct agent HTTP/form handoffs. Scoped
[actual Loom/graceful behavior](shuttle-massage-instant-runtime-qa-2026-10-19.md)
is verified with retained QA probe failures and read-back reconciliation. Actual
Loom/native handoffs, owner-reminder presentation and real Telegram acceptance
remain open; source migration50 / `mvp-57` is uninstalled on the schema49 /
`mvp-56` primary. Atomic rescheduling remains a later complete workflow.

Telegram specialist-access approval now displays the canonical human terms
without a redundant machine-argument block. [Source and actual runtime proof](shuttle-specialist-approval-readable-qa-2026-10-18.md)
retain exact internal argument/hash binding and plain conversational consent.
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

## Verified component and remaining gates

The Linux Go 1.27.2/PostgreSQL 17 race gate passed 12 roots with zero failures/skips:
seven binding/atomic-revocation cases and five neighboring schedule/booking cases.
It checks grant/revoke/regrant, original receipt replay after newer changes and
expiry, changed-body rejection, IAM callback failure, expired approval, multiple
aliases per human, forbidden transfer, removal/readdition, revoked target/module,
receipt-write rollback, schedule-write rollback and stale grant rejection after
schedule replacement. Both runs preserved source and all ten main Compose
fingerprints. Exact Go/PostgreSQL containers and private volumes were removed;
no OOM occurred. Limits were 2 GiB for Go and 512 MiB for PostgreSQL and no swap.

The first compile found an invalid error symbol; it was corrected to the existing
scoped missing-row convention. First lint reported one exported comment prefix;
that was corrected after the first 11-root gate. Final configured DB/integration
lint reports zero issues. Independent correctness/quality and fresh final source
reviews found no actionable findings. Their additional stale-proposal coverage
suggestion is included in the final 12-root gate. Evidence is retained under
`.cache/shuttle-work/massage-staff-store-v1-qa/` and `massage-staff-store-v2-qa/`.
This is binding store acceptance, not full staff authorization or real-service QA.

## Shared staff reads

The agent and Mini App Store entry points share current-authority event discovery,
schedule, timetable/client-list and own-client detail implementations. Read HTTP
routes, binding writes and manifest methods are implemented in source.
Both specialist read and administrative forms have source component acceptance
below; owned forward installation passes, while full real-Telegram workflow acceptance remains open. No participant pass is required. Disabling the massage module retains staff reads; public specialist IDs,
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

## Shared-read component verification

The final Linux Go 1.27.2/PostgreSQL 17 race gate passed 21 roots and 28 passing test
events with zero failures/skips. Seven new roots cover no-pass staff access,
event-wide versus own contacts/detail, several aliases without duplicate events,
module disabling, deployment isolation, grant/local identity revocation, native
session/generation/expiry and single-connection use, revocation/logout after an
event-lock wait, equal-start chronological continuation, filter-bound cursors,
range overlap, cancellation exclusion, retained past bookings, frozen terms and
byte-trimmed pages without missing rows. Neighboring binding, schedule, booking
guard and native handoff/lock-wait cases also pass. Initial two source snapshots
passed the same 21 roots/28 events; their lint failures remain recorded. Four
initial lint issues and then one cognitive-complexity issue were corrected by
normalizing the typed SQL page limit once. Final configured DB/integration lint
reports zero issues. Independent correctness/quality and fresh final reviews
found no actionable findings.

All three runs preserved their frozen source and all ten main Compose service
fingerprints. Their exact temporary Go/PostgreSQL containers and private volumes
were removed, with no OOM. Limits were 2 GiB/512 MiB, respectively, and no swap.
Evidence: `.cache/shuttle-work/massage-staff-reads-v1-qa/`, `-v2-qa/`, `-v3-qa/`.
This accepts the Store component only. Agent/native HTTP, UI, real IAM/Loom/
Telegram specialist acceptance, full massage parity and production remain open.

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
verified below. Write HTTP is verified in the later section. The specialist read
form has source acceptance; the administrative form also has source acceptance below. The
owned Loom preparation wire accepts only `prepared` with a future expiry; do not
invent a `completed` preparation status or renew an expired canonical approval
for recovery. The actor-scoped proposal-status read supplies completed recovery.
Binding write tools and native routes now implement this verified HTTP contract.
Schema48 is installed on the owned stand. Preserve its migration bytes; any
further schema change needs a new migration and the corresponding acceptance.

## Read HTTP component verification

The final Linux Go 1.27.2/PostgreSQL 17 race gate passes 31 roots and 38 passing test
events: 27 integration roots/34 events and four API roots/events, with zero failures
or skips. Five new integration roots exercise no-pass non-admin staff across all
four agent/native read types, own versus foreign contact/detail, grant revocation,
no extra agent task, native admin configuration/target reads, agent target lookup,
role denial, revoked-target diagnostics, origin/event/limit/session rejection,
same-operation binding effects/replay across interfaces and logout while a binding
write waits for the event. Existing Store, schedule, native booking and global
admin discovery checks also pass. Four API roots include consumer-readable
manifest parsing and bounded timetable/cursor schema validation.

The initial 31-root gate was green but did not cover an omitted `own_only` field.
Independent final review found that Huma required it although discovery made it
optional. The query field is now optional while the cursor field stays required;
the final gate includes a real agent HTTP request omitting the filter and verifies
that it returns the same event-wide records. This was a review finding, not a
failure caught by the initial test suite. Earlier lint runs found 26 then two
schema-construction issues; final configured DB/API/integration lint reports zero.
Independent correctness/quality and fresh final review report no actionable
findings after the optional-filter fix. Their earlier finding remains retained.

Both gates preserved frozen source and all ten main Compose service fingerprints,
with no OOM. Their exact temporary Go/PostgreSQL containers and private volumes
were removed. Limits were 2 GiB/512 MiB with no swap. Evidence:
`.cache/shuttle-work/massage-staff-api-v1-qa/` and `massage-staff-api-v2-qa/`.
This is mocked-peer HTTP plus real-PostgreSQL component acceptance, not real IAM/
Loom/Telegram specialist acceptance, new forms, fresh-operation handoff or rollout.
At this read gate, runtime was schema47 / `mvp-51` and source was schema48 /
`mvp-52`. The later binding HTTP milestone below used `mvp-53`; current
source is `mvp-54`.

The later owned forward installation now passes at schema48 / `mvp-54`.
Next exercise native/agent/mixed paths with real IAM/Loom/Telegram before accepting
the complete feature; full live-controller rollback remains unproven. No production activation is authorized.

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

The Linux Go 1.27.2/PostgreSQL 17 race gate passes 35 roots/44 test events:
31 integration roots/40 events and four API roots/events, zero failures or skips.
Four new roots cover fresh-operation native-to-agent and agent-to-native handoff,
concurrent application, original receipt replay after revoke/expiry, exact
proposal/arguments/actor isolation, expired adoption, alias-insert rollback,
failed authorization without effects, expired native sessions and discovery of
all 21 proposals across a page boundary after cursor expiry. The previous
31-root read HTTP/binding/schedule/native booking suite also passes.

Independent quality review found that the first bounded pending list hid entries
after twenty; pagination and the 21-proposal test correct that finding. Correctness
and two fresh final reviews report no actionable findings. Initial lint/typecheck
failures (complexity, formatting, an invalid embedded initializer and sqlc column
ambiguity) were corrected before the final gate. Final DB/integration lint has
zero issues. The frozen source and all ten main service fingerprints stayed
unchanged; no OOM occurred. Both exact temporary containers and their private
PostgreSQL volume were removed. Evidence:
`.cache/shuttle-work/massage-binding-canonical-v1-qa/`.

This is Store/component acceptance with mock peers, not real-service handoff or
activation. At this Store gate, source was schema48 / `mvp-52` and runtime was
schema47 / `mvp-51`. The later binding HTTP milestone below used `mvp-53`; current source is `mvp-54`.
The later HTTP section adds agent/native write support. The specialist read UI
now has source acceptance, as does the administrative form. Complete real IAM/
Loom/Telegram native/agent/mixed QA remains open.

## Reactive administrator context verification

An explicit user request now includes up to five canonical pending specialist
access proposals and five original receipts, newest first in each list. Interface
aliases cannot duplicate those records. Pending proposals must be unexpired and
created before the accepted task. Completed effects must have their original
receipt timestamp before that task; a preparation created earlier but applied
later is excluded from that task's receipt history. A module being disabled does
not erase historical results.

The task path first reads only an authenticated actor's history-presence bit.
When history exists, it resolves the caller's IAM principal and requires the
current `zinusya.admin` role in this deployment before reading private payloads.
The Store rechecks current local identity and task/actor/event deployment scope.
`RecentMassageBindingContext` itself requires the caller to perform IAM global
administrator authorization; it does not implement its own IAM client. This uses
the existing token lifecycle/cache policy. The role-loss test explicitly
invalidates that user's token cache before the next request.

Context is available to a new reactive Loom run and to steering of an existing
run after a bot restart. Creating a preparation, adopting it or applying through
the native Store path creates no agent task. Prompt instructions classify this
history as untrusted metadata, never consent or a new request; require current
state/status reads, exact human approval, unchanged arguments and original expiry.
Original receipts describe old effects, not present access. No session bearer,
IAM token or extra user profile is included.

The Linux Go 1.27.2/PostgreSQL 17 race gate passes 41 roots/50 test events:
37 integration roots/46 events and four API roots/events, zero failures or skips.
Three new roots cover 5+5 bounds, before-task cutoffs, post-task application,
expiry, missing/foreign tasks, actor/deployment isolation, module-disabled history,
no proactive run, canonical alias deduplication, explicit creation and steering
after restart, original receipts and withholding context after role loss with
cache invalidation. Existing owner-massage and order-review reactive context
paths also pass, alongside the previous canonical/read HTTP tests.

Configured DB/task/integration lint reports zero issues. Independent correctness,
quality and fresh final review report no actionable findings. Frozen source and
all ten main Compose service fingerprints stayed unchanged; no OOM occurred.
The exact temporary Go/PostgreSQL containers and private database volume were
removed. Evidence: `.cache/shuttle-work/massage-binding-context-v1-qa/`.
This uses real PostgreSQL and the actual bot task loop with mock IAM/Loom/Telegram
peers. It does not accept real-service specialist handoff or install the feature.

At this context gate, source was schema48 / `mvp-52` and runtime was schema47 /
`mvp-51`. The later binding HTTP milestone below used `mvp-53`; current source is `mvp-54`.
The following HTTP component exposes write/status/pending tools. Both forms now
have source acceptance; next complete real IAM/Loom/Telegram native/agent/mixed QA
before accepting the complete feature.

## Exact agent and native binding HTTP contract

The binding HTTP milestone introduced these tools in `mvp-53`. Current source
manifest `mvp-54` dynamically advertises three additional global
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

The final Linux Go 1.27.2/PostgreSQL 17 race gate passes 46 roots/55 test events:
41 integration roots/50 events and five API roots/events, zero failures or skips.
Four new integration roots cover native preparation -> fresh agent adoption/apply,
agent preparation -> fresh native adoption/apply, canonical pending discovery,
original status/replay after revocation, completed preparation reconciliation,
explicit false/zero/confirmation rejection, origin and idempotency failures,
expired consent, malformed selectors, non-admin denial on all new paths and IAM
role loss while apply waits for an event lock. The complete mixed HTTP path uses
one real PostgreSQL connection and an intake acceptor that fails if invoked; no
agent task or proactive run is created. Queue workers are not started in this
one-connection fixture. The API wire-schema root verifies exact
confirmation/status/idempotency paths, optional adoption metadata and required
explicit false/zero fields. Existing binding/staff/owner/order-context tests pass.

Two failed gates remain preserved. The first passed its API unit roots but
integration server construction panicked: the original native operation-status
route overlapped the existing event schedule route under Go ServeMux. The fixed
`status/operations` and `status/proposals` namespace has disjoint fixed second
segments. Textual reviews before that gate did not catch the overlap. The second
gate passed 45 roots, but the one-connection test fixture incorrectly started the
task queue, which requires at least two connections. The fixture now builds the
actual HTTP API directly on the limited Store; queue requirements and business
checks were not relaxed. The final gate repeats all 46 expected roots.

Initial compile/lint failures were corrected before the gates: one missing
wire-field constant and seven constant/shadowing findings. Final configured
API/task/integration lint reports zero issues. Independent correctness/quality
reviews and fresh final review of the corrected source report no actionable
findings. The second gate explicitly recorded unchanged source/main state. The successful
final gate retained frozen source and all ten main Compose fingerprints, with
no OOM. Failed resources were explicitly aborted/removed; the final
exact Go/PostgreSQL containers and private volume were removed after verification.
Evidence: `.cache/shuttle-work/massage-binding-http-v1-qa/`,
`massage-binding-http-v2-qa/` and `massage-binding-http-v3-qa/`.

This is actual HTTP plus real PostgreSQL with mock IAM/Loom/Telegram peers.
It does not accept specialist/admin UI, a real Loom write conversation or real
Telegram mixed handoff. That HTTP milestone used source schema48 / `mvp-53`, uninstalled; the owned
runtime at that checkpoint was schema47 / `mvp-51`. The specialist read form milestone follows below;
owned installation now passes, while real-Telegram native/agent/mixed acceptance remains open. No production readiness is claimed.


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

The final source gate passed 48 Linux/PostgreSQL race roots / 57 pass events,
143 Mini App JS tests under Node process TZ=Pacific/Honolulu, zero-issue targeted
lint and independent correctness/quality/final reviews. A proposed switch to frozen
booking display timezone was rejected against the explicit event-local display
requirement; the differing-zone UI test covers this decision. Earlier failed gates
remain preserved: a `_windows.go` filename implicitly excluded the new files on
Linux, then a test compared time representations rather than equal instants.

Loopback browser QA used actual HTML/CSS/JS with mock data. It verified event
selection, own detail/contact href without following it, foreign contact isolation,
DST offsets, Russian labels/prices, own-client filtering and clearing after grant
revocation/session expiry. A preview-only bootstrap syntax error was fixed before
that inspection. This is not real IAM/Loom/Telegram or unattended-model acceptance.
The preview server was stopped and its temporary tab closed. Full evidence and
remaining gates are in [the form QA record](shuttle-massage-staff-ui-qa-2026-10-18.md).

At that form checkpoint the staff feature was uninstalled on schema47. It is now
installed at schema48 / `mvp-54`; the administrator form has source acceptance below.
Real-Telegram native/agent/mixed acceptance remains next. No production readiness or deployment is claimed.


## Native administrator binding form

The Mini App administrative form now has source component acceptance. No new API
or migration is introduced: source and the owned runtime use schema48 / `mvp-54`.
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

The source gate passes six selected Linux/PostgreSQL race roots (four critical
binding HTTP cases plus two API/asset/schema cases), 167 Mini App JS tests including
24 administrative cases, zero-issue API lint and independent correctness, quality
and fresh final review. This targeted gate builds on the earlier 48-root staff
component gate; it is not a new full-Go-suite or real-service acceptance claim.
Reviews found and fixed the overly strict revoke schedule check and hidden startup
errors, with behavioral coverage. Three early failed PII-clearing assertions were
caused by an inaccurate DOM fixture; its `replaceChildren` now clears text as the
browser does. No product workaround was added for that fixture problem.

Actual-assets loopback browser QA passed grant, revoke, RU preview, synthetic
agent-proposal continuation, a lost reply after application followed by page-reload
recovery (effect count unchanged at three), initial outage recovery, server-reported
consent expiry, and clearing after administrator revocation. This used mock data,
not real IAM/Loom/Telegram. The temporary tab/server and exact QA containers/private
volume were removed. See [the administrative form QA record](shuttle-massage-admin-ui-qa-2026-10-18.md).

Forward installation is now accepted on the owned stand at schema48 / `mvp-54`.
Native administrator and roleless specialist real-Telegram QA now pass. Next
perform agent-only administrative writes and both form/agent approval handoffs. Complete live-controller rollback, full massage parity, unattended-model behavior
and production readiness remain unaccepted. Production and release publication remain unauthorized.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
