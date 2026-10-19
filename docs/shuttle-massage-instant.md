# Specialist instant self reservation

Updated: 2026-10-19. Domain/store, HTTP and isolated shipped-form gates pass.
[Real Loom agent-only/graceful behavior](shuttle-massage-instant-runtime-qa-2026-10-19.md)
and both actual Loom/native handoffs are verified, with retained restart QA
observer failures and independent read-back reconciliation. The primary remains
schema49 / `mvp-56`; the built instant migration50 / `mvp-57` candidate remains
uninstalled. The separate [upcoming-session workflow](shuttle-massage-specialist-next.md)
is current source schema52 / `mvp-59`: timed delivery and native checkboxes pass
component and [real IAM/Loom runtime QA](shuttle-massage-specialist-next-runtime-qa-2026-10-19.md).
That Linux candidate is built and uninstalled. Runtime revoke/regrant also rejects old pending consent.
Original committed receipt recovery after private binding loss and current
shared-table competition also pass real Loom QA. Owner-reminder presentation,
real Telegram WebView acceptance remain open; specialist-before-session runtime QA
is recorded separately.

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
loss; nullable cancellation booking references remained rejected. The exact
disposable container and volume were removed with exit0 and no OOM. This direct
DDL check is separate from a Goose rollout and full application acceptance.

The Go domain package passes eighteen test roots, including six new roots for
grid/grace boundaries, frozen time, configured lengths, short grids, work gaps
and the repeated DST hour. Seven focused integration roots pass on a new empty
PostgreSQL, including three new instant roots and four existing booking,
configuration, binding and staff-read paths. The integration harness applies
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

The domain and migration each have independent clean source reviews. The store
has three fresh diff reviews with no actionable findings. The configured full
store/domain/integration lint passed after formatting; the final integration
test check also passes. The ordinary form script passes Node syntax validation.

Ignored evidence: `.cache/shuttle-work/specialist-instant-v1-qa/`, especially
`migration-checkpoint.json`, `store-checkpoint.json`, `store-tests-v4.jsonl`,
`model-tests-v2.jsonl` and lint logs. Failed attempts remain alongside them.
Owned primary domain data and installed runtime were preserved. No real Telegram
messages, production changes, staging, commits or publication are part of this
checkpoint.

## HTTP and native form acceptance

The agent tool `zinusya_massage_staff.instant` exposes prepare/apply/status under
`/internal/agent/massage_staff/instant`. Preparation returns normalized arguments,
the original proposal/expiry and localized human terms. It accepts no caller start
time, owner, purpose or binding generation. The manifest is `mvp-57` in source.
Authenticated native prepare/apply use
`/app/massage-staff/{event}/instant[/prepare]`. Shared owner pending, history,
operation receipt and cancellation routes retain the same durable records.

Eight focused HTTP/store integration roots and seventeen API unit roots pass.
They cover native-only writes, forbidden caller fields, prepared terms and both
HTTP/native handoff directions. The configured API/task/integration lint passes;
the final lint also includes the opt-in `browserqa` fixture. Independent HTTP
source review has no actionable findings.

The explicit browser gate uses the shipped HTML/JavaScript, Go HTTP and a separate
PostgreSQL. It uses mock IAM/Loom peers, a signed fixture Telegram SDK and a fresh
Edge context. The driver blocks other origins. This is not actual Loom tool
execution, a model conversation or real Telegram acceptance.

The final browser run verifies:

- A non-administrator specialist without a pass sees only their owned alias.
  Both configured short and six-block services appear; this browser run books the
  short service. Domain tests separately cover configured lengths.
- An injected409 from current schedule configuration leaves retained windows,
  timetable and own-history entry usable while disabling a new self reservation.
  This is a UI failure injection, not a real event-date-edit acceptance test.
- The native preview shows self-reservation terms, event-local time and timezone,
  price and expiry before explicit confirmation. Ordinary slot selectors are
  hidden for this no-pass workflow.
- After the server commits the first booking, its successful HTTP reply is
  deliberately lost. Reload recovers the original operation receipt, with full
  result equality and no second apply POST.
- Direct agent HTTP preparation continues in the native form with the same
  proposal, normalized arguments, expiry and frozen displayed terms. Native
  confirmation and the original agent apply return the same full booking.
- Native preparation continues through direct agent HTTP adoption/confirmation.
  Native confirmation then returns that same complete original receipt.
- Explicit owner cancellation releases the first two bookings. The ledger has
  three canonical self bookings (two cancelled, one booked), five preparations,
  seven operation records and six retained owner reminder records. All three
  booking snapshots and arguments match their original preparations. No agent
  task/run or initial specialist booked notice is created; two cancellation
  notices remain under the normal binding/preference rules.

Screenshot inspection found a previous cancellation receipt beneath a fresh
preview. New preparation/event selection now clears it; the final browser gate
asserts the receipt is empty and the corrected screenshot was inspected. An
initial browser attempt also rejected the valid service display order before
creating any proposal or booking; its failed result is retained. The next run
passed the behavioral gate before that visual correction; it is not the final UI
acceptance result.

Source review fixed a historical-timetable regression and QA evidence-retention
issues. Two fresh reviewers then reported no actionable findings; a fresh final
review also passed after the visual correction. The final browser test root
passes without skips. Each temporary PostgreSQL exited0 without OOM and its
container/volume were removed. Failed-schema archives and the complete JSON
business ledger are retained privately. After a successful Go test, fixture
schemas are dropped, so the small post-test dump is not a business-data backup.
Primary installed files, runtime identity and domain data remain preserved.

Ignored evidence: `.cache/shuttle-work/specialist-instant-api-v1-qa/` and
`.cache/shuttle-work/specialist-instant-ui-v{1,2,3}-qa/`. The final UI proof is
`specialist-instant-ui-v3-qa/test-checkpoint.json`, its browser log, result,
ledger and screenshots. Public source entry points include
`internal/api/massage_instant.go`, `internal/api/miniapp_massage_instant.go` and
the opt-in `integration/massage_instant_browser_test.go`.

## Remaining complete workflow

1. Check owner-reminder presentation and late-booking parity: runtime QA observed
   both kinds firing with identical visible text for one late reservation.
2. Perform real Telegram/WebView acceptance. Joint CA/zitadeltg verification
   remains reserved for the agreed session with Daniel.
3. Record integrated review and deployment proof before primary installation.

The [specialist upcoming-session workflow](shuttle-massage-specialist-next.md)
now implements Python `notify_specialist` / `notify_next` behavior through the
separate specialist notice ledger. The owner reminder ledger retains only
`owner_long` and `owner_short`; their presentation policy is unchanged.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
