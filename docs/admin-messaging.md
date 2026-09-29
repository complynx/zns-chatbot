# Administrator messaging: historical domain baseline

The current implementation contract is [Administrative broadcast parity](broadcast-parity.md). It includes durable input hints, agent tools, Go templates, authorized profile filters/reads and complete paged audiences without the former 1000-recipient cap. Candidate27 is under independent acceptance; built does not mean accepted.

The text below records the earlier domain/runtime baseline and its then-outstanding work. Its limits and remaining-work list are historical, not current recommendations or migration status.

## Historical baseline contract

`adminmessage.Service` uses PostgreSQL migration 040. The current global
administrator list is `core.pass_booking_admins`, shared with existing global
pass administration. Payment administrator status alone grants no access.
Preview, enqueue, cancel, audience resolution, and results recheck that list.
Only the owning administrator can act on a draft or read its results. The
service-only claim and completion methods must never become user-facing tools.

1. Resolve explicit recipients or `$event[:category]`, then call `Preview` with
   the actor's identity and a stable operation key. Preview persists a normalized,
   deduplicated immutable destination/content snapshot without sending.
2. Show the saved snapshot to the administrator. Call `Enqueue` only for the
   explicit send intent. A changed request under the same key conflicts.
3. The worker calls `Claim`, sends exactly that content/destination, then calls
   `Complete` using the returned attempt. Record either a Telegram message ID or
   a bounded failure summary. Retryable failures wait at least 30 seconds and honor a valid Telegram cooldown. Terminal
   failures remain visible separately from successful sends.
4. `Cancel` prevents pending sends; it cannot recall an in-flight Telegram call.
   Current ACL is checked when work is claimed. Revocation stops unclaimed work;
   an already claimed network request may finish.

Supported content: plain text, Telegram HTML, legacy Telegram Markdown, or an
explicit source-chat/source-message forwarding reference. Destinations are
nonzero numeric chat IDs or public `@usernames`, optionally with a positive
topic ID. Adapters must ground each explicit destination and forwarding source
in administrator input; never invent either from model output. Format validity
and destination existence are ultimately checked by Telegram. Text is limited
to 4096 UTF-16 units, requests to 1000 supplied destinations, keys to 200 bytes.

Pass categories preserve the Python selection meaning: default `assigned`
includes assigned and paid; `unpaid` selects assigned; `paid` selects paid;
`waitlist` selects waitlisted and pending-couple registrations; `all` selects all
noncancelled bookings. Cancelled Go tombstones are excluded from every booking
category because Python cancellation deletes the registration. `admins` includes hidden payment
administrators. Existing migration normalizes the legacy `payed` spelling.
Expansion is a snapshot: enqueue never silently adds later registrations.
An expansion exceeding 1000 recipients fails instead of truncating.

Leases last two minutes; the sender must use a shorter network deadline.
Expired leases can be reclaimed after restart. Attempt numbers reject stale
completion writes. Telegram has no send idempotency key: a crash after Telegram
accepts a message but before completion is recorded can duplicate the send.
This is at-least-once delivery, not exactly-once. Ambiguous network failures
need an explicit runtime policy before production use.

## Historical remaining-work list (superseded)

- Equivalent intent-grounded agent tools remain; the explicit command and bilingual controls are implemented below.
- A durable pending-input hint with expiry, explicit attachment of a message or
  forward source, and cancel. Unrelated text/media must follow normal routing;
  a pending draft must never consume the next update automatically.
- Legacy MongoDB query recipient selectors, aliases, and
  Python/Tornado templates. None are silently interpreted by this domain.
  Template parity needs a constrained data-only design and explicit evaluation
  of stored profile fields, user links, names, and informal-name behavior.
  Arbitrary template code/network execution is not part of the contract.
- Current runtime global administrator configuration must populate the reused
  ACL consistently. Enforce service-only authorization for worker endpoints.
- Telegram-like bilingual functional acceptance remains required. Sender throttling, bounded retries and sanitized delivery summaries are implemented below.

The integration tests use synthetic PostgreSQL data and exercise no external
Telegram send. They cover durable preview/replay, idempotency conflicts,
deduplication, enqueue replay, recovery fencing, terminal perrecipient results,
cancellation, ACL revocation, shortcut ACL, and destination syntax validation.

## Runtime increment (2026-09-26; functional acceptance pending)

The runtime now accepts `/send_message_to <recipient...> --msg "text"` and
`--html` / `--md`. Quoted JSON recipient arrays, numeric/public username targets,
positive topic IDs, and `$event[:category]` selectors are supported. Preview is
persisted by update key; send, cancel, and results buttons enforce current global
administrator ACL and draft ownership. English and Russian catalog messages are
used. Long previews/results are split before the action card. Ordinary free text
continues to the assistant; this increment installs no pending-input interception.

API paths under `/v1/admin-messages/` require a user identity. Claim/completion
under `/internal/admin-messages/` require the separate delivery credential. The
bot worker claims at most one destination per polling cycle, uses a 20-second
send deadline, and sends the immutable snapshot literally. Telegram rejection
summaries omit raw upstream descriptions. Only explicit 429 responses retry
(up to three attempts, at least `max(30, retry_after)` seconds apart); ambiguous transport or decode
failures stop with `telegram_outcome_unknown` for administrator inspection. Structured cooldowns outside 0–86400 seconds stop with `telegram_invalid_cooldown`; they are never silently shortened. Invalid JSON cooldown values remain ambiguous transport/decode failures.
Restart lease recovery retains the documented at-least-once crash limitation.

Remaining coherent slices: (1) explicit attachment/forward-source selection with
expiring durable hints and intent-grounded agent tools; (2) typed legacy profile
selectors and data-only personalized templates, including stored names/links and
informal-name semantics. The Python alias map is currently empty. Arbitrary Mongo
queries and executable Tornado template expressions remain unsupported and fail
closed, so this increment does not complete Python parity. Full bilingual
Telegram-like functional QA remains required.
