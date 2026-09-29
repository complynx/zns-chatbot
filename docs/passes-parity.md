# Passes: active Python parity inventory

Source inspection: 2026-09-25. This is a discovery inventory, not acceptance of
the Go implementation. No production records or private configuration were read.
References below use repository-relative paths and source line numbers from this
inspection. `P` means `zns-chatbot/plugins/passes.py`.

## Reachability and configuration

The plugin is active in `zns-chatbot/plugins/__init__.py:8,18`. Its registered
commands are `/passes`, `/passes_assign`, `/passes_cancel`, `/passes_tier`,
`/passes_switch_to_me`, `/passes_uncouple`, `/passes_table`, `/legal_name`,
`/passport`, and `/role`; callbacks use `passes|...` (`P:2457-2472`). The timeout
processor starts in the constructor (`P:2475`). These are not historical APIs.

Events supply visible and hidden payment admins, localized long/short titles,
passport requirement, finish date, assignment rule, tiers and optional concurrency
override (`zns-chatbot/events.py:20-48,134-191`). Tier fields are amount, positive
price, start, promo, and blocked-by-date. Default rule is `distributed`; an invalid
rule normalizes to `paired`. Sell start is the earliest tier start, or infinity
without tiers (`events.py:154-170`). Event activity uses finish date; active and
all-event lists are distinct (`events.py:202-205,266-296`, `P:2477-2500`).

## User commands and conversations

| Flow | Active behavior | Evidence |
| --- | --- | --- |
| `/passes` | Lists active events with localized titles and exit. Selecting an event first detects invitations, then displays an existing pass, otherwise begins registration. Existing passes remain viewable before sales open; only new registration has the sell-start guard. | `P:547-581,829-872` |
| New registration | If passport data is required, asks legal name then passport. Otherwise skips both. Then selects leader/follower, payment admin when multiple visible admins exist, and solo/couple. One visible admin is chosen automatically. | `P:829-849,1066-1085,1113-1274` |
| `/legal_name` | Requests a legal name, offers the stored value and cancel, writes the global user profile, confirms, and stops. It does not create a pass. | `P:873-875,1066-1085,1171-1206,4610-4611` |
| `/passport` | Uses the default event, asks name then passport where required; callback can specify an event. Invalid/inactive event gets a beginning/registration message. | `P:1099-1111,1134-1209` |
| `/role` | Shows current profile role when present, leader/follower/cancel buttons. Writes profile role only; existing pass role is a snapshot and is not updated by this command. | `P:1276-1297,1517-1550` |
| Solo | Creates `waitlist`, `solo`, profile role, signup timestamp, payment admin, then recalculates queues. Switching from pending couple or reopening a solo preserves prior signup timestamp, pass role and valid admin. Assigned/paid/accepted-couple callbacks are rejected by the state guard. | `P:924-968` |
| Couple request | Available to a new registration, pending invitation or unlinked solo waitlist. Requests a forwarded message; rejects self and assigned/paid invitees. Saves inviter as `waiting-for-couple`, preserves prior signup metadata, and sends invitation if the invitee is known. Unknown invitees can encounter it on event entry. | `P:547-577,877-913,970-1064` |
| Accept invitation | Runs required identity input; copies inviter pass into two reciprocal `waitlist` couple records, gives invitee the opposite role, informs both, recalculates queues. | `P:419-461` |
| Decline invitation | Checks inviter link; removes it, changes pending inviter to solo waitlist and presents solo/couple choices. | `P:463-539` |
| Pass card | State-specific text; change-name and exit always. Pay only when assigned; make-couple only for unlinked solo waitlist; make-solo only while waiting for couple; admin change and cancellation hidden when paid. | `P:205-266` |
| Change payment admin | Lists visible admins. Selection persists in profile before registration or current pass afterward. Hidden admins are not offered. | `P:268-363` |
| User cancellation | Refuses paid; otherwise deletes own pass and accepted partner's nonpaid pass, notifying partner. Pending invitation cancellation affects only inviter. Recalculates queues after success. | `P:379-394,1552-1581` |
| Input cancellation/timeouts | Uses a tagged cancel reply-keyboard item; removes reply keyboard on name/passport/proof completion/cancel/timeout. Couple input has its own cancel/error/timeout feedback. | `P:712-754,915-922,970-997,1066-1200` |

User identity fields belong to the user, scoped by bot. Passes are stored by
bot, user and event (`P:3681-3724`). A couple is two participant records, not one
record with quantity two. Price is stored per participant. These distinctions
must survive import and the Go database model.

## Assignment, pricing, promo and locks

The active states are `waiting-for-couple`, `waitlist`, `assigned`, `paid`.
`paid` means proof was uploaded, or a free pass was assigned; it does not by
itself mean admin approval. Approval has separate timestamp/admin fields
(`P:605-710,726-827,1407-1448`).

Queue behavior (`P:4051-4334`):

1. Sort waitlist applications by signup time then user ID; split by pass role.
2. First try the minority role's top solo when out of balance.
3. If both queue heads are solo and currently balanced, try both. If a head is
   a couple, try the oldest head couple.
4. Fall back to the first solo for target role, then other role. This fallback
   can scan past a couple. Couple fallback considers queue heads only.
5. Recollect statistics after assignment. Double-solo recollects between the
   two assignments and explicitly allows one successful assignment to remain
   if the other fails (`P:4556-4580`).

Balance is paid plus assigned, with higher role at most 52% except totals zero
or one. When already imbalanced, single/couple moves must not worsen absolute
or proportional imbalance; double-solo requires balance before and after
(`P:2620-2674`). Balance exclusions use `skip_in_balance_count`: explicit false
includes even a free pass; otherwise true or zero price excludes it
(`P:3668-3679`). Capacity statistics still count assigned/paid participants,
including balance-excluded records (`P:3008-3103`).

The unpaid-assignment ceiling is 10 participants unless disabled for the event;
a couple or double-solo needs two free slots. Full counts, not balance-filtered
counts, enforce it (`P:30,4156-4174`). Recalculation uses one in-process async
lock and a pending flag to coalesce requests and rerun before unlocking
(`P:2473-2474,4051-4068`). This is not a cross-process/database transaction lock.

Pricing rules:

- Distributed tiers share amount across roles; a couple uses the same tier.
  Paired tiers have `amount // 2` capacity per role; couple members can receive
  different tiers (`P:2677-2693,3541-3651`).
- Start dates provide a tier floor. Nonblocked future tiers can become effective
  as earlier capacity sells out. A future `blocked_by_date` tier stops scanning,
  including when it is promo (`P:2835-2976`).
- Promo tiers are available to solos, not couples. Promo here is a tier flag;
  no user promo-code command or redemption flow is registered
  (`P:2457-2472,2905-2910,3633-3645,4500`).
- A distributed couple may use the current tier when exactly one slot remains
  and a later eligible tier can be opened. Both retain current-tier pricing;
  without a later assignable tier this exception is unavailable (`P:2960-2976`).
- An explicit admin price is a total split by sorted user ID with integer
  remainder awarded to lower IDs. Displayed couple price sums stored individual
  prices. Defaults are first tier, event price, then a keyed legacy fallback;
  automatic defaults must be positive (`P:2536-2601`).
- Assignment stores timestamp, per-user price, tier index and one-based tier
  number; existing tier markers are preserved. Zero-price participants become
  paid with a `free_pass` proof and accepted timestamp. A couple involving a
  free participant splits into solo records (`P:1407-1477`).
- A stale couple can fall back to solo under specific assignment paths; a
  partner still waiting for invitation acceptance prevents automatic assignment
  (`P:3541-3651,4423-4554`). Do not treat all missing partners identically.

## Payment, permissions and operations

Pay is offered only for assigned passes and starts a photo/document conversation.
Other input gets an error. The largest photo or document file ID is recorded;
proof upload conditionally changes matching assigned participant records to paid,
sets proof-received time/current and receiving admin, forwards evidence to that
admin, then sends review buttons and user confirmation (`P:582-603,726-827`).

Event payment admins include visible and hidden admins. Both can approve/reject
proofs, even if not the recorded receiving admin. Global admin membership alone
is not the proof-review authorization check (`P:605-609,659-663,3766-3782`).
Accept keeps paid and adds acceptance metadata; reject returns both linked
participants to assigned and records rejection. Both remove the review keyboard
and inform the submitting user (`P:605-710`). Current ambassador, receiving
ambassador and approving admin are distinct fields.

| Admin command | Authorization and effect | Evidence |
| --- | --- | --- |
| `/passes_assign` | Global admins. Event required; optional total price/type/comment, create-last/create-name/leader/follower, mutually exclusive skip/append-to-tier. Can create a missing application; forces assignment past date blocks. Existing assigned/paid recipient is reassigned, splitting a linked couple; partner retains individual price. Positive replacement price clears proof fields and returns assigned; zero creates accepted free pass. Append increases a valid current/earlier tier by successful participant count. | `P:1671-1985,3268-3297` |
| `/passes_cancel` | Global admins. Deletes named recipients including paid; if only one member named, surviving partner becomes solo. Recalculates queues. This differs from user cancellation. | `P:2078-2160` |
| `/passes_uncouple <event> <user>` | Global admins. Requires both couple records; removes both links and changes both types to solo without changing stored per-person prices. | `P:2011-2076` |
| `/passes_tier [--pass_key ...]` | Global or event payment admin. Defaults to default active event; reports current pricing/capacity, role balance, upcoming tier gates and couple eligibility. | `P:1987-2009,3299-3539` |
| `/passes_switch_to_me` | Global or event payment admin; explicit event may include inactive events. Updates linked participants' current ambassador and notifies them. `--received_only` only backfills missing receiving admin on paid records and does not replace existing provenance. | `P:2162-2312` |
| `/passes_table` | Global admins receive active events; event payment admins receive only their authorized active events. Sends XLSX with participant/profile/state/couple/pricing/tier/payment/provenance/comment fields. Couple total and per-one price are separate. Paid records missing receiving admin fall back to current admin. Passport number is not an export column. | `P:2314-2441` |

## Background behavior and legacy compatibility

Startup migrates `payed` to `paid`, fills missing payment admins, replaces invalid
waitlist admins (including partner notification), prompts assigned/paid users
missing required passport data once, then recalculates (`P:3821-3970`). Lazy
embedded-pass migration remains active in `get_user_with_pass` (`P:3709-3724`);
the bulk `migrate_embedded_passes` call is commented out (`P:3838`). The historical
spelling is not an additional active state.

The periodic loop sleeps an hour per active event before processing. First
payment reminder occurs after six days from assignment; second occurs one day
after the first reminder; cancellation occurs two days after the first reminder.
These are marker-relative deadlines, not an unconditional eight-day assignment
cutoff. Invitations expire after 2 days 10 hours and convert inviter to solo
waitlist, notifying both (`P:34-39,1600-1669,3971-4049`). Remaining waitlisted
participants receive a once-marked no-more-passes message even when the blocker
is date/concurrency rather than permanent sellout (`P:1583-1598,4336-4362`).

Registration announcements remain active: per-record sent marker, event thread
and locale, Telegram user name and role. This scans records without requiring
paid state (`P:4364-4419`).

`sputnik` exists in `PASS_TYPES_ASSIGNABLE` (`P:40`), but the current UI offers
only solo/couple and no registered sputnik creation flow exists. Custom admin
type is active; the constant alone does not prove a separate product flow.
The event role-cap configuration exists, but the inspected assignment loop uses
tier capacities; do not invent an independent cap gate from the field name.

## Source limitations: decisions for port review

These are observed code paths, not requirements to reproduce defects:

- Invitation acceptance validates inviter existence but does not revalidate the
  current target link/state before copying records (`P:436-451`). Stale or forged
  callbacks need explicit Go authorization/state checks.
- The input condition still requires `MessageOriginUser`; a bare contact is
  rejected despite partial contact checks (`P:977-999`). The proven functional
  contract is forwarded-user messages, not general contact acceptance.
- Frozen-name checks skip to the continuation; when passport is required that
  continuation can return to passport request recursively (`P:1068-1069,
  1115-1116,1201-1206`). Preserve the lock intent, not recursion. No freeze/unfreeze
  command is registered in this plugin.
- Pair writes are multiple database operations. Assignment may delete both
  participants when matching is incomplete (`P:1450-1490`); this is not evidence
  of transaction safety. Go pair changes need PostgreSQL race/failure proof.
- Some UI-only guards are absent from mutation handlers (for example changing
  payment admin from a stale paid-pass button, `P:314-326`). Hidden buttons are
  not authorization.
- Export uses a shared `passes.xlsx` filename (`P:2431-2441`). Export concurrency
  safety is not established by this source inspection.
- Admin command/status prose and export headings include inline English; the
  existence of EN/RU catalogs does not prove full localization.

## Existing evidence and recommended next slice

Existing Python assignment tests cover queue priorities, partial double-solo
success, balance, concurrency override, blocked promo, same/split couple tiers,
overflow exception, stale partner fallback, sales opening and admin reassignment
(`tests/test_passes_assignment_framework.py:477-899,1078-1699,1712-2017`). They use
fake persistence and can skip when runtime imports are unavailable
(`tests/test_passes_assignment_framework.py:15-40,156-466,472`). They are useful
behavior examples, not real database or Telegram acceptance evidence. No tests
were run for this source-only inventory.

Recommended first independently reviewable slice: **pass discovery and individual
profile conversations**. Implement `/passes` active-event selection and existing
pass read cards, `/role`, `/legal_name`, and required-passport conversation with
persisted input context, safe cancel/timeout and restart behavior. Stop the
creation journey at a clearly identified handoff until solo/couple registration
and queue assignment are delivered together in the next slice. Do not mark
registration or the whole passes stage accepted based on this slice. A card for
an existing fixture may show status but must not offer unimplemented actions.

This slice establishes event-specific identity requirements and independent
profile commands without pretending that creating a solo application is complete
without automatic assignment. Subsequent reviewable scopes are registration plus
couples/queue/pricing; proofs/payment/deadlines; admin operations/export/import.
Each needs both independent QA gates before acceptance.

Public acceptance cases for the first slice (give Functional QA this scope and
fixtures, without implementation details):

1. In both English and Russian, `/passes` lists active localized events and
   excludes a finished event. Selecting a future event refuses new registration;
   an existing participant can still read their pass.
2. `/role` shows the saved selection, can change/cancel, and survives restart.
   Changing profile role does not silently change an already registered role.
3. `/legal_name` offers the previous value, saves/acknowledges new input and
   removes the reply keyboard. Cancel, timeout, duplicate update and restart
   do not change another conversation or another user's profile.
4. A passport-required event asks name then passport; an event without the
   requirement skips both. Frozen profile fixtures do not loop or permit edits
   to locked data. Passport values are absent from unrelated cards/exports/log
   assertions available to the stand reviewer.
5. Reopen old buttons after cancel, event expiry or profile change. UI reflects
   current state; forged cross-user/event callbacks cannot edit another profile.
6. EN and RU cover all prompts, buttons, cancellation, errors and timeout text;
   no raw catalog keys, inline fallback English in RU, or HTML injection from
   user names. Event title locale fallback is deterministic.
7. Verify through Telegram-like messages, callback edits, reply keyboards and
   input flows on one fixed sandbox build. Prove persistence and isolation with
   real PostgreSQL integration tests using testify assertions; test relevant
   duplicate/concurrent inputs rather than only HTTP status codes.

Catalog source anchors: `i18n/en/bot.ftl:441-745` and
`i18n/ru/bot.ftl:479` onward; event title locale selection is in
`zns-chatbot/events.py:94-130`. Go acceptance must cover both catalogs and all
newly translated administrative prose; source localization gaps are not waivers.

## Pure allocation decision slice (integration pending)

`platform/internal/passallocation` ports balance and tier eligibility only. It is
not called by the runtime yet and does not establish stage acceptance.

- `Counts.Balanced` and `Counts.Allows` preserve the 52% threshold, initial
  zero/one exception and non-worsening absolute/proportional imbalance. Strict
  mode is for double-solo only (`P:2627-2674`). In particular, a couple at 10/7
  is allowed in ordinary mode although it does not restore balance; strict mode
  rejects it. The queue docstring's "strictly lower" wording is less precise
  than the executable helper.
- `Counts.TargetRole` selects the minority when imbalanced; otherwise the larger
  role waitlist, with ties favoring the smaller assigned/paid count then leader
  (`P:3653-3665`). `HasConcurrencyCapacity` enforces ten unpaid assigned
  participants, with the event override (`P:4156-4174`).
- `PickTier` preserves role-half versus total capacities, the latest-started
  configured tier floor, promo exclusions, date barriers, future-tier activation
  and the distributed couple one-slot overflow exception (`P:2677-2976`).
  Effective usage is the maximum of explicit tier counts and usage implied by
  total participation after subtracting all preceding configured capacity.
  A future tier becomes effective when projected participation exceeds prior
  capacity or the nearest positive-capacity predecessor is full. A blocked
  future promo tier is a barrier before promo filtering. Overflow checks later
  nonpromo positive capacity up to the first date barrier; it does not run the
  full future-tier effectiveness predicate again, matching Python.

Integration contract:

1. Normalize event assignment rules before calling; this typed API rejects
   unknown rules rather than silently applying a configuration fallback. Preserve
   configured tier order and provide nonnegative, bounded database counts and
   positive configured prices. Resolve event timezones into absolute times and
   pass one fixed `Now` per decision. The functions do not validate imported
   configuration or untrusted request bodies.
2. Build balance `Counts` separately from full tier `Usage`. Apply Python's
   tri-state balance-exclusion semantics in the persistence layer; explicit
   false includes zero-price records, absent excludes zero-price records,
   explicit true always excludes (`P:3668-3679`). Full usage includes them.
3. Supply role-specific `Usage` for paired decisions and total usage for
   distributed decisions. `Explicit` keys and returned indexes are zero-based.
   A paired couple needs two independent calls, one per role with increment one;
   a distributed couple needs one call with increment two. Set `Couple` for
   both cases to prohibit promo prices. Use `tiers[index].Price` per participant.
   Solos use increment one. `IgnoreDateBlocks` is for an authorized administrator
   path; this package itself grants no permissions.
4. Check balance and concurrency separately, re-read current participant state,
   validate reciprocal partners, and commit in the service's transaction/lock.
   These helpers reserve nothing, mutate no inputs and provide no race safety.
   Double-solo remains two sequential decisions with fresh statistics after the
   first result; its allowed partial success must not be mistaken for a couple.

Unit fixtures derive from
`tests/test_passes_assignment_framework.py:653-709,802-921,1078-1136,1332-1699`.
They exercise boundary counts, date-blocked promos, historical implied usage,
explicit usage, time floors, overflow, per-role couple pricing and concurrency.
They do not execute the Python test suite or claim differential parity.

Still unported in this package: waitlist sorting/candidate iteration and retries,
couple invitation/state recovery, statistics queries and balance exclusion,
transactional assignment, price splitting/free-pass transitions, admin actions,
notifications, payment/deadline processing, tier status carryover display,
imports, UI and localization. PostgreSQL and Telegram Functional QA remain
required after runtime integration; unit/lint success is not those gates.

Focused validation on 2026-09-25, from `platform`: `go test -count=1
./internal/passallocation/...`, `tools.local/golangci-lint.exe run
./internal/passallocation/...` (pinned 2.14.0, zero issues), and `go vet
./internal/passallocation/...` passed. The pinned formatter was also run on this
package. Windows required separate temporary Go/lint caches and execution with
workspace path access because the restricted default cache/path lookup failed.
