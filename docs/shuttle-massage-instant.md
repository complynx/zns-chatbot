# Specialist instant self reservation

## Legacy behavior and scope

Python `plugins/massage.py` uses instant booking to reserve the specialist's own
time: the specialist is both owner and specialist. It does not create a booking
for an arbitrary client. Preserve that boundary. A current private event/alias
binding authorizes this path without a pass or ordinary party booking limit.
Public aliases and administrator status alone do not authorize it.

The server selects the current grid slot relative to the chosen party window.
It advances once if the slot is more than five minutes old. It does not search
for a later gap. The legacy party tolerance is strictly two hours before/after
the window. Signed floor matters before the window: with twenty-minute blocks,
exactly fifteen minutes before the window still selects an invalid negative slot;
just later it advances to the first slot. Configured service lengths, work spans,
specialist/owner occupancy and table capacity still apply. Existing topology
checks remain; this slice does not settle overlapping-window resource policy.

There is no new payment, refund, currency conversion, third-party booking,
rescheduling or generic permission bypass.

## Durable contract

`MassageInstantArguments` accepts the event, expected event/schedule versions,
and window/alias/service IDs. It accepts no user, chat, start time, purpose or
binding generation. The server stores the selected time in canonical terms and
the private binding version in the snapshot. Optional `purpose: staff_self`
distinguishes this record; absent fields preserve historical ordinary snapshots.

The existing preparation and operation tables use a distinct `instant` kind.
Preparation creates neither booking nor capacity hold. Same-operation preparation
returns the original proposal and expiry, even if new consent would now be needed.
Adoption revalidates that exact proposal without selecting another slot or
extending its life. An ordinary booking route cannot consume an instant proposal.

Application uses the existing operation -> event -> actor/session lock order.
Before a new effect it checks the current own active binding and generation,
enabled module, event/schedule versions, exact frozen terms, database clock,
proposal expiry and occupied capacity. Revocation and regrant do not revive old
consent. A stale proposal must fail rather than move to another time.

Original committed receipts and consumed-proposal replay return the original
result without creating another booking, including after grant loss. Current
local identity/session checks still apply. Existing owner cancellation remains
available after grant loss and releases the same booking. Alias ownership grants
no authority to cancel another client's booking.

The self path suppresses only the initial specialist booked notice. Existing
owner reminder records remain. Cancellation follows the existing
current-binding/preference rules: an active recipient can receive cancellation;
a revoked alias receives no notice.

Store entry points exist for agent/owner and authenticated MiniApp preparation,
adoption and application. Pending proposals expose a separate typed `instant`
argument field. The shared owner form now displays these choices and their exact
self-reservation preview. The specialist form supplies its own-alias/service
selector and opens that shared confirmation, recovery and cancellation workflow.

## Migration and verification

Migration50 widens three existing CHECK constraints. It adds no table or data
rewrite. Its Down locks both affected tables and refuses retained instant
preparation or operation history before restoring the older constraints.

On a separate PostgreSQL restored from the synthetic two-customer schema49
archive, transactional Up/Down preserved all ordinary rows and original checks.
Instant preparation and operation records independently blocked Down without
loss; nullable cancellation booking references remained rejected.
This direct
DDL check is separate from a Goose rollout and full application acceptance.

The Go domain package passes eighteen test roots, including six new roots for
grid/grace boundaries, frozen time, configured lengths, short grids, work gaps
and the repeated DST hour.
The integration harness applies
embedded migrations through the normal store migrator in isolated schemas.

New integration coverage checks no hold during preparation, cross-interface
adoption/application and receipt replay, private ownership, revoked/regranted
bindings, distinct ordinary consent, table capacity, expiry, stale event version,
retained reminders and cancellation behavior. These are Windows Go1.27.2 CGO0
component tests with mock IAM/Loom/Telegram peers, not Linux/race, actual Loom
tool dispatch or real Telegram acceptance.

Earlier retained test attempts failed on test comparisons of `time.Location`,
a missing second alias in a moved schedule fixture, an incorrect expectation of
a notice after grant revocation, and shared fixture work-span storage. The final
tests compare wire terms and preserve independent alias spans. These failures
are not recorded as successful acceptance runs.

The domain and migration each have independent clean source reviews.
The ordinary form script passes Node syntax validation.

Failed attempts remain alongside them.
Owned primary domain data and installed runtime were preserved.
No real Telegram
messages, production changes, staging, commits or publication are part of this
checkpoint.

## Remaining complete workflow

1.
Check owner-reminder presentation and late-booking parity: runtime QA observed
   both kinds firing with identical visible text for one late reservation.
2.
Perform real Telegram/WebView acceptance.
Joint CA/zitadeltg verification
   remains reserved for the agreed session with Daniel.
3.

The [specialist upcoming-session workflow](shuttle-massage-specialist-next.md)
now implements Python `notify_specialist` / `notify_next` behavior through the
separate specialist notice ledger. The owner reminder ledger retains only
`owner_long` and `owner_short`; their presentation policy is unchanged.
