# Pass registration domain slice

This is an implementation under review, not accepted full Python parity.
Discovery, registration and profile API/Telegram wiring now exist; agent and
payment flows have separate gates. The source contract is
[the active passes inventory](passes-parity.md).

`internal/passbooking.Service` exposes authenticated-owner `Get` and `Execute`,
plus a global-booking-admin `Queue` view. `Execute` takes an actor separately
from its command. Adapters must derive that actor from authentication and must
never accept it from interpreted model parameters. Participant records contain
no legal-name or passport copies: registration checks the existing
`core.pass_profiles` record and saves its role as a separate pass snapshot.

The new migration defines event/tier configuration, explicit global booking
admins, visible/hidden payment admins, applications and hashed idempotency
operations. This follows the current one-bot-per-database deployment model of
`core.users` and `core.pass_profiles`. No production data is imported or modified.
Configuration adapters and admin provisioning are not implemented here.

Supported owner commands are `solo`, `invite`, `accept`, `decline`, `cancel` and
`payment_admin`. `invite` takes a forwarded user's numeric Telegram ID, allowing
an unknown recipient to join later. `accept` and `decline` require both the
target inviter and its expected version; the current invitation must name the
authenticated recipient. Acceptance creates reciprocal participants atomically,
uses the inviter's signup time/admin and gives the recipient the opposite role.
Paid/assigned/accepted-couple records cannot be overwritten by stale registration
commands. Switching a pending invitation to solo preserves signup time and role.

New registration requires open sales and an active event. Passport-required
events require both existing identity fields. A profile role is needed for a new
solo/inviter but not for an invitee whose role is derived from the invitation.
Existing records remain readable when sales are closed. Hidden payment admins
are not selectable; an existing valid hidden admin can remain on an application.
With one visible admin it is chosen automatically; multiple choices require a
selection. This slice does not persist a profile-level default payment admin.

Owner cancellation refuses paid participants and cancels both reciprocal unpaid
partners. Global-admin `admin_cancel` can cancel a paid participant and leaves a
surviving partner as a solo at its unchanged per-person price. `admin_uncouple`
requires reciprocal participants and removes links without repricing them.
Global-admin `recalculate` applies current configuration to the queue. Authority
is read from `core.pass_booking_admins`; payment-admin membership alone does not
grant these operations. Proof review uses event payment-admin membership as
described in [payment processing](pass-payments.md).

Every mutation locks its event row before reading applications. The owner and
relevant profile are read under share locks. Versions reject stale commands;
all affected participants receive a new version. Cancelled applications retain
a tombstone/version to prevent an old callback becoming valid after recreation.
Idempotency keys and command payloads are hashed, scoped by event and actor;
replays return the current owner record instead of an old snapshot. All mutation,
allocation and operation-marker writes commit together. Configuration writers
must lock the event row before changing tiers or payment admins. Imported data
must be validated separately and must not bypass this locking contract.

Queue recalculation uses `internal/passallocation` for balance, concurrency and
tier eligibility. It orders applications by signup time and numeric Telegram ID.
It tries the minority head solo, balanced head double-solo, oldest head couple,
then first eligible-role solo and head-couple fallbacks. Double-solo pricing is
sequential with fresh statistics and permits the first participant to remain
assigned if the second has no available price. Couple writes are atomic.
Distributed couples share a tier; paired couples price each role independently.
Assigned/paid statistics include all participants for capacity; balance uses the
tri-state exclusion rule, including explicit false for a zero-price participant.
Broken historical couple links can fall back to solo, while a partner still
awaiting invitation acceptance blocks assignment. New domain operations do not
create such broken links.

Still required for full parity: event discovery/localized titles and configuration
adapters; notification outbox/delivery; user/admin UI and EN/RU flows; payment
proofs, provenance and review; free/forced assignments and custom admin pass
types; append-to-tier, price overrides and receiving-admin backfill; reminder,
invitation expiry and registration-announcement jobs; export/import and MongoDB
cutover. There is no claim that unit tests or this document replace independent
Code QA and Telegram-like Functional Senior QA.

Focused integration tests use the existing `database(t)` helper, creating one
fresh disposable PostgreSQL database per test. They cover pair persistence and
restart, role snapshots, replay/stale guards, unknown/forged invitees, permissions,
concurrent accept/cancel, sequential double-solo capacity and admin cancellation.
The first focused PostgreSQL run passed all five cases on 2026-09-25. Additional
boundary/failure tests and pinned lint are in progress. No Functional QA has run.
