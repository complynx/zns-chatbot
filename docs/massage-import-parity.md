# Massage import preservation contract

Status: built; independent Code QA and Functional QA are required. This document is
not acceptance or cutover permission. Runtime legacy draft compatibility is a
separate review scope implemented alongside this importer.

## Source authority

Baseline inspected: `zns-chatbot/plugins/massage.py`, SHA-256
`80708a909c2ac25ce8cf3b41a110f5c5382984404cff090745a246cea88db1ba`.
Use this checked-out source, not prices from an older memory or another branch.
`price_from_length` returns BYN 43/57/90/110/125/150 and RUB
1200/1600/2500/3000/3500/4000 for lengths 1–6. Python does not store price.
Duration is length * 20 - 5 minutes. The importer derives these values and
preserves every original record. This does not authorize changing business rules.

Active sources and readers:

- `MassageRecord.init/save`, finalization and cancellation (massage.py:270–405,
  622–744): massage collection, ObjectId, user_id, pass_key, created_at, length,
  party day, slot, start/end, specialist, finalized_at, deleted.
- Draft editor (650–884): choices, specialists_choices, page, transient error,
  selected length/party. These are active persisted workflows, not historical
  garbage. Exact raw values survive and the normalized draft feeds the runtime
  callback adapter. Finalization removes choices/filter/page.
- Specialist lookup (104–249, 1260ff): same-bot users.massage_specialist with
  name/icon/about locale variants, min/max duration in slot units, work_hours,
  table_not_required, notify_bookings, notify_next. Despite its name,
  table_not_required truthiness selects table-consuming specialists.
- Reminder loop (1128–1180): deleted and sent flags use field **existence**.
  false/null still suppress a reminder. notify requires true. This is different
  from the truth-value preference flags on specialists.
- Effective configuration supplies parties with start/end, massage_tables and
  is_open; massages.max_massages_a_day and reminder intervals. Events collection
  alone cannot supply these facts. Parties retain source array order. Duplicate
  Minsk day numbers are blocked because Python's day map cannot represent both
  unambiguously.
- The source has no massage receipt/payment media fields. A massage manifest
  proof is blocked as unmapped. No proof is silently dropped.

Unknown fields and unsupported representations fail closed. Known source fields
are retained in raw provenance even when they do not drive the SQL projection.
Records from a positively identified other bot are retained as excluded source
facts; absence of booking bot_id is normal Python and requires bot-namespace
attestation. Specialist users always require a same-bot identity.

## Snapshot and mappings

The existing verified immutable offline stage remains the only source. Required
included domains: users, events, massage, configuration. Configuration records
with kind `legacy_massage` use this exporter-attested shape:

```json
{
  "_id": "massage-config",
  "kind": "legacy_massage",
  "event_key": "grodno_26",
  "daily_limit": 3,
  "prior_long_seconds": 3600,
  "prior_short_seconds": 600,
  "parties": [
    {"start":"2026-12-01T18:00:00+03:00", "end":"2026-12-02T02:00:00+03:00",
     "massage_tables":1, "is_open":false}
  ]
}
```

Do not manufacture configuration from booking dates or SQL defaults. Each event
requires an imported event source receipt. Each participant/specialist requires
an imported user receipt matching the exact source hash and same-bot Telegram
identity. No name/email matching, eligibility inference, or administrator grant
is performed. Global source specialist profiles are projected into each expressly
configured massage event, including preferences and all work spans.

All source dates must be exported as explicit instants, with operator attestation
of their actual source wall-clock interpretation. Python writes naive
`datetime.now()` for created/finalized timestamps and Minsk wall time for booking
slots; blindly treating every naive BSON date as UTC is not correct. The importer
rejects naive strings. Booking start must equal configured party start plus
slot*20 minutes. Stored end must match duration. No clipping or repair occurs.

IDs: booking/draft `legacy-massage:<bot>:<object-id>`; party
`legacy-massage-party:<event>:<day>`. Raw ObjectIds and source hashes persist in
`core.legacy_massage_import_references`. Specialist source fields remain pending
in users import until massage projection completes transactionally. Source raw
records, excluded records, derived runtime rows, pending obligations and receipts
are reconciled together.

Source has no cancellation or reminder-delivery timestamp. SQL cancelled_at and
sent_at use snapshot captured_at as a migration marker, never a claim that the
source event happened then; exact source flags remain authoritative provenance.
Instant creation lacks created_at, so finalized_at supplies the required target
created_at. Instant is inferred only for that source constructor shape with
owner=specialist; a normal self-booking retains instant=false. Booking-created and
cancellation notices are not newly emitted for imported history. Existing sent
flags suppress reminder replay. notify=true creates a pending additional notice
only for a nondeleted booking starting on/after captured_at.

Historical sent-marker notices have delivery_state=sent, remain unbound to a bot
or delivery queue, and retain captured_at as their existing migration timestamp
marker. No Telegram message ID or new delivery success is invented. An eligible
additional notice binds the resolved owner, snapshot bot and same-bot Telegram
chat in its owner row and shared delivery queue within the import transaction.
Queue registration is the final phase after all owner/source writes; any failure
rolls back both the domain projection and its queue binding.

Draft normalized state is `{party,length,page,selected,choices}`: party is the
target party ID (empty if unselected), absent length becomes zero; selected keys
are mapped specialist owners; choice keys remain source numeric strings and
values preserve party selection, page delta, slot+specialist or specialist toggle.
Legacy slot pagination uses24 choices per page, matching Python
`SLOT_PAGE_SIZE = SLOT_PAGE_SPLIT * 6` with `SLOT_PAGE_SPLIT = 4`. This differs
from the modern eight-choice menu. Preserve the stored page; clamp the displayed
page to current availability, and offer navigation when an adjacent page exists.
Deleted drafts remain closed. Raw transient error and missing-vs-present details
stay in source evidence. Modern version starts at zero and is runtime-owned.

## Forward, replay and rollback

`PlanMassage` emits a private hash-bound artifact; `ApplyMassage` regenerates it
from the verified stage and requires version1 resolutions with plan_sha256,
dates_verified, configuration_verified, bot_namespace_verified, writers_stopped.
`ReconcileMassage` requires the same plan/resolution pair and performs no writes.

Apply is one transaction for the complete configured massage slice. It takes the
shared import advisory lock and target-table write locks, resolves source-bound
identities, inserts into empty event scopes, records source evidence and completes
deferred specialist obligations. Existing event scopes or references cause a
conflict, never overwrite. Any missing dependency, malformed draft, constraint
failure or receipt failure rolls back all target rows and obligations. Restart
reapplies the same artifact; successful exact replay only verifies the captured
projection and identity mappings. Changed source/resolution/target state fails
reconciliation, including draft versions and preference changes. The receipt
captures the exact imported notice IDs. Historical marker rows remain exact;
additional notices retain exact ID, booking, owner, kind, creation time, bot and
chat. Their queue owner/effect, bot, destination/thread, lane sequence and traffic
class remain exact. Missing or changed bindings fail without repair.

Delivery owns additional-notice attempt, lease, deadline, failure, text, sent and
followup progress, plus queue state/not_before. Reconciliation permits changes
to those delivery fields and does not claim to audit their transition history.
Later runtime reminder IDs are outside the imported notice set. Other imported
booking, draft, configuration, identity and source projections remain exact.
Replay verifies the original receipt; it never rewrites it to accept drift.

No source collection is changed and there is no production connection. Rollback
before commit is automatic. After commit, use a disposable database restore or
restore the pre-import database backup with writers stopped. There is deliberately
no delete-based rollback against an active target: it could erase subsequent
bookings or callback progress. Preserve the snapshot and private artifacts until
cutover and rollback verification are accepted.

## Evidence and remaining gates

Focused real PostgreSQL tests exercise source hashes/identities, specialist
preferences, booking price, existence-based reminder suppression, normalized
drafts, completed obligations, exact replay, draft drift rejection and rollback
for unresolved specialist choices. They use disposable databases on the local
sandbox PostgreSQL service. They are not a production-data claim or Telegram GUI
acceptance. CLI dispatch and independent runtime/UI acceptance remain separate.

## Runtime continuation contract

Imported source ed/back/xed callbacks can mutate a draft only at version zero;
continuation uses persisted modern version-scoped buttons. Source choices are not
renumbered. Explicit false filters survive; missing filters use runtime defaults.
Existing booking execution owns availability, eligibility, limits, price and
notification rules. A failed booking leaves the draft resumable; successful
booking, draft closure and replay receipt commit together. Owner-scoped finalized
booking callbacks resolve the source reference and use existing cancellation with
imported version one. Other old practitioner callbacks are outside this adapter
slice and remain a required parity/acceptance inventory item.

CLI commands: `plan massage --stage ... --out ...`, `apply massage --stage ...
--plan ... --resolutions ...`, and `reconcile massage` with the same apply flags.
Database connection comes only from `MIGRATE_DATABASE_URL`. Output contains counts,
hashes and fixed diagnostic codes; identifiers and draft content stay private.

## Public legacy callback grammar

The prefix is `massage`; components are separated by `|`. IDs are original
source document IDs, resolved within the authenticated owner/event/bot namespace.
They are references, not authorization. Malformed, foreign or stale callbacks
must not mutate another user's state.

| Callback | Meaning |
| --- | --- |
| `massage|start` | Home |
| `massage|exit` | Close interaction |
| `massage|create` | Start booking selection |
| `massage|ed|<id>` or `massage|ed|<id>|0` | Resume imported draft or show booking |
| `massage|ed|<id>|<positive integer>` | Draft duration before duration is chosen; afterward a stored choice key. For a finalized booking, choice1 is cancellation. |
| `massage|ed|<id>|<negative integer>` | Draft back action |
| `massage|back|<id>` | Go back in draft |
| `massage|xed|<id>` | Cancel draft or owned booking |
| `massage|notifications` or `massage|notifications|0` | Specialist notification preferences |
| `massage|notifications|51` | Toggle booking notifications |
| `massage|notifications|52` | Toggle next-client notifications |
| `massage|instant` or `massage|instant|<length>` | Specialist self-reservation; omitted length is1 |
| `massage|clientlist` or `massage|clientlist|<day>` | Specialist clients; day is original party day-of-month |
| `massage|sped|<booking id>` | Specialist's exact client booking card |

The imported draft's choice map determines the meaning of a positive selection;
QA must preserve that map from its input fixture, not guess an appointment from
a numeric key. Modern rendered buttons carry current versions and remain the
normal continuation after a legacy entry. Preferences/client cards/instant
operations require the current specialist role; ownership and live availability
are rechecked for booking changes. Telegram update identity supplies retry
identity; duplicated delivery must not apply a toggle or booking twice.

### Imported party eligibility and reminder markers

`parties[].is_open` preserves the source flag. A true value excludes that party
from customer massage slot selection; it does not mean customer-bookable.
Existing imported appointments and their history remain readable. Drafts whose
saved party is now ineligible must recover through current eligible choices
without treating their old choice map as permission to book.

Source reminder markers are `client_notified_prior_long` (long client reminder),
`client_notified` (short client reminder), and `specialist_notified` (next-client
notice). Presence, including false or null, records already-handled history.
Absence permits normal current eligibility. The separate boolean `notify` enables
an additional client notice only when true. The specialist flags
`notify_bookings` and `notify_next` are preferences, not per-booking sent markers.
The source `deleted` field also uses presence, including false, for deletion.
