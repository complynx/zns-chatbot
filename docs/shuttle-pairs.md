# Pair registration design

Pair consent is a separate aggregate.
Pairing must never combine balances, rewrite
an assigned price or mark a submitted proof as paid.

Migration 00015 adds schema and domain types.
The store now supports invitation,
acceptance, decline, revoke and immutable operation replay.
Solo assignment checks
pending outgoing holds and active membership in both preparation and execution.
The agent API exposes participant-only invitation reads.
The background service
releases expired invitations from durable database state.
The store supports exact
paired assignment preparation and confirmation, exposed through an authenticated
API and dynamic agent manifest.
Participant metadata reads and uncoupling are also
exposed through the API and agent manifest.
Directed invitation writes, participant
listing, policy context and operation receipts are exposed.
Durable invitation
delivery is implemented.
The first native pair milestone used schema 17 and
manifest mvp-13; the historical tier-pricing checkpoint used schema 18 and mvp-14.
Native participant uncoupling and bot restart now pass with
a confirmed synthetic partial payment and exact preservation of both owner passes.
This accepts the first bounded pair milestone, not the full stage 3 lifecycle.

## Current source verification

Focused Linux/PostgreSQL race tests cover competing acceptance, explicit target
consent, invite holds, stale solo proposals, decline/revoke release, explicit expired
invitation resolution, scheduled expiry after restart during simultaneous IAM/Loom
outage, original request times, replay after restart and revoked
actor access. API tests cover both consent participants, unrelated users and an
unrelated global administrator. Invitation metadata contains no pass or money.

The read endpoint is POST /internal/agent/registrations/invitation, advertised as
invitation in zinusya_registrations. Its arguments are event_id and invitation_id;
the caller identity comes from the verified user token. Missing and inaccessible
invitations both return 404. Revoked local accounts are denied before reading.
This endpoint does not discover a target or deliver an invitation.

Current global or event administrators can prepare and confirm paired pass
assignment; payment ambassadors alone
cannot assign passes.
Preparation freezes both participant account IDs, current
event price/currency, payment window, deposit minimum and event-local final deadline.
Without a tier policy, each participant uses the configured event price.
Optional
[tier pricing](shuttle-tier-pricing.md) freezes and rechecks each owner's price,
tier and deposit quote; paired role quotas can produce different participant prices.
Confirm requires the same actor,
operation, proposal and argument hash, and rechecks event/pair/registration versions,
active membership, active accounts, both role capacities and expiry after locks.
Both pass snapshots share one final database instant.
Each remains a separate owner
pass with its own deposit record.
A committed receipt replays after proposal expiry,
but current administrator access is still required.
This is manual mailbox-provider QA, not autonomous model evaluation.

The zinusya_pairs module exposes participant metadata and uncoupling tools to active
users. Assignment and its prerequisite reads are discovered only for a current
global or event administrator. Its events read returns public event
metadata under that authority. Its candidates read returns waitlisted pairs,
participant account IDs and the exact argument versions needed for assignment in
the requested authorized event. Both reads use UUID pages, default/maximum 20;
follow next as after. Candidate order is not waitlist allocation priority. The read
does not reserve capacity or guarantee eligibility and includes no participant
pass or payment data. Copy the selected candidate's arguments into preparation.

The assign tool uses POST /internal/agent/pairs/assign/prepare
and POST /internal/agent/pairs/assign with matching operation_id/idempotency_key,
exact_parameters confirmation and the returned proposal_id. Status and reconcile
use the same path suffixes and accept only protocol_version and operation_id, as
Loom's status backend requires. The event comes from the actor-scoped immutable
preparation. Capacity, stale proposal and other business conflicts return an explicit
terminal rejection; transport/storage failures remain separate. A not_found result
means no visible committed receipt and must not trigger a blind replacement operation.
Status exposes both frozen pass records only after current assignment authority is
checked; consent participants use their own registration reads for financial data.

Expiry resolves through a participant decision, an inviter's new invitation attempt,
or the background database sweep. The sweep runs in the service watch loop, with
its own one-second context and pages of at most 100 expired invitations. It needs
neither user authorization refresh nor a Loom run. The event lock serializes expiry
with consent and assignment; each transaction rechecks state and database time,
then advances the invitation and inviter registration together. It runs even when
an account is revoked or the registrations module is disabled. Historical operation
receipts remain unchanged. Pending rows may briefly be past expires_at until the
sweep succeeds; they still hold the inviter out of solo assignment. A database
failure leaves the row available for the next sweep or application restart.

## Consent and membership

A directed invitation names an existing local target account in the same deployment.
It holds the inviter out of allocation without reserving or mutating the target.
There is one pending outgoing invitation per inviter; incoming invitations may
compete.
Username search and Telegram-ID enumeration are not introduced.
The owner can read `zinusya_pairs.partner_code` to share their own account UUID
and, when verified Telegram bot metadata is available, a deep link.
Raw UUID
sharing remains supported.
Opening the link gives the agent an untrusted target
hint; it creates no invitation, hold or registration.
The opener may subsequently
explicitly invite the code owner, who must separately accept.
The code identifies
the target but is not authentication, proof of sharing or consent.
No directory
or rotating-secret table is introduced.
A code alone does not complete delivery
or target acceptance.

The invitations read in zinusya_pairs returns incoming or outgoing invitation views
scoped to the current participant. It defaults to incoming/pending; state=all reads
history, and optional event_id filters a specific event. UUID pages default/max 20,
with next used as after. Each view includes current invitation state/version/expiry,
event identity and IANA timezone/dates, the inviter's account code and public Telegram
display name, and required_role. It does not include financial records or a profile
directory. A name is display data, not proof of identity or an instruction.

Before inviting, invitation_context supplies the caller's waitlist role/version and
expiry. The agent must explain the allocation hold before an explicit invitation
request. This read reserves nothing and does not guarantee future eligibility.
The invite, accept, decline and revoke writes use matching operation/idempotency keys
and none confirmation. Acceptance requires the intended target's explicit agreement
to join this event in required_role; a share code cannot authorize it. Decline/revoke
remain available for cleanup when the module is disabled; new invites/acceptance
still require enablement and valid deadlines. Target eligibility failures are generic
and do not reveal another account's pass or profile.

Each write has operation-only status/reconcile suffixes compatible with Loom.
These
return actor- and kind-scoped original receipts after current identity checks.
A
creation receipt may still say pending after the target accepted: use invitations
for current state.
Missing status is unknown and must not cause a blind replacement
operation.
Migration 16 adds durable notification intents in the same transaction
as invitation creation, decisions, and lazy or background expiry.
A storage failure
rolls back the invitation effect and its receipt.
The target receives pending and
revoked intents; the inviter receives accepted, declined, and expired intents.
Each invitation version and recipient has one intent, including after replay or
restart.
Ready-intent scans are deployment-scoped and bounded to 100 records.

The River worker repairs committed intents with a bounded cyclic cursor. Before
sending it verifies the recipient through IAM, then rechecks local access and the
invitation version/state under the event lock. Stale or expired pending invitations,
disabled-module pending invitations, revoked accounts and accepted invitations whose
pair has dissolved are suppressed. Cleanup notices remain available after module
disablement. Messages use plain text, en/ru with the common language fallback, and
event-local expiry with the IANA zone. They include the event/invitation references;
pending notices identify the inviter and required role and request explicit consent.

The attempt ID and payload commit as send_inflight before Telegram IO.
Known
retryable rejection follows Retry-After; ambiguous sends become delivery_unknown
without automatic resend.
Startup recovery runs under the exclusive deployment
lock.
Focused PostgreSQL tests use mock IAM and Telegram; they do not establish native
delivery.
Native probes verify the existing CA/hostname, expected bot metadata and
local rejection of forbidden recipients; those probes send no Telegram messages.
Separate native QA accepted invitation delivery, consent, assignment and safe
uncoupling with restart.
Other delivery-resolution branches and the broader stage 3
lifecycle remain open; see the native pair QA report linked above.

Global administrators can inspect pair_notifications and pair_notification under
/internal/agent/administration. Lists are deployment-scoped UUID pages, default/max
20, with state filtering. Detail includes delivery state/version, attempt ID, receipt
and current invitation/event context; it excludes the persisted Telegram payload.
This context is current metadata, not an immutable snapshot of what was sent.

The existing resolve_notification prepare/write/status/reconcile flow accepts
notification_kind=pair. Omitted kind retains the old payment behavior and serialized
arguments, so historical payment proposals and hashes remain compatible. Kind is
part of the exact arguments and cannot be swapped in a prepared operation. Retry
or dismiss requires current global administrator authority, the observed notification
version, a reason, and the exact unexpired proposal. An unknown retry warns that the
message may already have arrived. Dismiss closes delivery without proving receipt.
Neither action changes pair consent, registration, passes or money. Retry creates a
new generation; the worker still rechecks current recipient and invitation access.
Replay and operation-only status retain the original resolution receipt after later
delivery, proposal expiry or application restart, with current administrator checks.

Migration 17 expands the existing audit table with a separate pair-notification FK
and an exactly-one-target constraint. Existing payment rows retain their FK and
receipts. Binary rollback keeps audit data; schema rollback refuses while pair
resolution history is retained. Export and plan its disposition before intentional
schema rollback.

Only the authenticated target can accept or decline. Acceptance checks both active
accounts, event/module access, expiry, opposite roles and unassigned waitlists.
It may create the target's opposite-role waitlist only through that target's consent.
An incompatible existing role conflicts rather than changing silently. Existing
assigned passes, active memberships and competing invitation holds must be checked.

Accepted pairs have two active members with unique roles. SQL constraints enforce
one active pair per registration, same-event membership and matching registration
roles. Store writes must also check that members are exactly the pair's recorded
leader and follower. A pair retains its original participant IDs when dissolved;
removing active membership does not delete history.

Decline, revoke and expiry release the inviter. Hold/release, acceptance and
dissolution advance affected registration versions. Both solo assignment preparation
and execution must reject held or paired registrations, including stale solo
proposals. No target registration is changed merely by an incoming invitation.

Both original requested_at timestamps remain intact. The provisional pair ordering
uses the later original timestamp, unlike legacy's copied inviter timestamp. This
choice is recorded for Daniel in ignored decisions; automatic allocation is not
enabled before the ordering policy is settled.

## Assignment and lifecycle

Pair assignment uses one PostgreSQL transaction and one post-lock database instant.
The scoped event lock serializes event effects; actor/participant eligibility uses
existing FOR SHARE user locks, in UUID order where multiple participants are checked.
Versions, capacity, closing gates and proposal expiry are rechecked after locks.
Both passes and independent deposit snapshots commit with one immutable operation
receipt. Never call two public singleton assignments in sequence.

Exact preparation shows both recipients and each participant's price, currency,
scale, deposit minimum and deadlines. Pricing must be a frozen quote from current
event/tier policy, not an unchecked agent amount. A small pass-snapshot writer can
be shared with singleton assignment; no generic command framework is needed.

Safe uncoupling preserves individual passes and money. Financially committed or
unresolved cancellation needs a separate explicit administrator policy; no automatic
refund, forfeiture or overdue-pass cancellation is introduced. Registration state
assigned does not mean unpaid: paid/deposit status is derived from confirmed money.
Lifecycle actions after sales closing need an access helper that does not blindly
reuse the registration sales-open helper.

The current store uncoupling operation requires an active participant and the pair
version seen by that participant. A global administrator who is not a participant
cannot use this operation as implicit impersonation. It permits cleanup after sales
closing or module disablement and when the other participant's account is revoked.
One event transaction dissolves the pair, removes exactly its two active memberships,
advances both registration versions and writes an immutable kind/hash-bound receipt.
Original participant IDs and requested times remain. Assigned passes, prices,
deposit snapshots, confirmed money and pending evidence are retained. An already
prepared pair assignment becomes stale. Repeating the original operation returns
its original result after restart; revoked actors cannot replay it. Administrative
forced uncoupling and cancellation/refund policy are separate future operations.

The my read at POST /internal/agent/pairs/my returns only the caller's active pairs,
with event name, dates and IANA timezone. It accepts optional event_id, after and
limit (default/maximum 20). A global administrator still sees only their own pairs
through this participant read. Dissolved pairs leave this active list but retain
their history and receipts. Neither partner profiles nor financial records are
included. Closed or disabled events remain readable for cleanup.

The participant dissolve tool uses POST /internal/agent/pairs/dissolve with the
selected pair's event_id, pair_id and expected_version, a matching operation key
and none confirmation. It acts only after an explicit user request. Status and
reconcile suffixes accept protocol_version and operation_id only. The receipt is
actor-scoped and current participant access is checked before returning it, including
after restart or re-pairing. Missing status is unknown. Business conflicts return
terminal rejection; permission and storage failures remain separate.

## Remaining stage 3 work and gates

The first cohesive milestone includes invitation, consent, owner-safe reads, durable
status/replay, atomic paired assignment and safe uncoupling. It is followed by tier
pricing, deterministic allocation, cancellation/deadline decisions and notifications.
The entire stage remains open until all of those are implemented and accepted.

Tier policy must retain legacy distributed versus independent-role tier semantics,
promo/date eligibility, effective usage and participant-specific quotes. A pure
planner should implement queue order and role-balance choices; the store rechecks
and commits a selected candidate under the event lock. Hard capacities and unpaid
assignment limits must use current decisions. Legacy permits partial double-solo
assignment; the new batch atomicity policy is a recorded open choice.

Critical behavioral gates include competing acceptances, accept versus solo assign,
pair assign versus singleton assign, zero partial writes when one role is full,
lock-wait crossing expiry/closing, stale membership/configuration, account revocation,
original receipt after restart/state change, owner privacy, unequal participant
terms, paid-money preservation and allocator tier/order/balance boundaries.

Native counterpart identities may be local IAM/API fixtures. Real Telegram sends
are restricted to the authorized current account in @complynx_test_bot. No invented
counterpart Telegram ID may receive a real invitation or notification.

Apply migrations before exposing the eventual pair candidate and do not mix old
solo-only writers with pair-aware writers once membership operations are enabled.
Binary rollback retains pair tables/history/receipts. Export before any intentional
schema rollback; migration Down drops new tables before removing supporting keys.
No registrations, passes, payment decisions or historical receipts are migrated or
deleted by the expand migration.
