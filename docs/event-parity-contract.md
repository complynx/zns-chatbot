# Event configuration and registration announcements (migration 058)

Implementation contract; independent Code QA and Functional QA remain required.
The frozen event foundation and pass-import candidate17 do not establish acceptance
of this expanded scope. No production migration or real Telegram send is authorized.

## Source behavior

- `zns-chatbot/events.py:21-47` defines tiers and configuration. `EventInfo.from_settings`
  preserves tier order and normalizes titles. `all_events` sorts by minimum tier
  start. `closest_active_event` chooses the latest started sale, otherwise the
  earliest future sale. An absent finish means active indefinitely.
- `events.py:34,132,176`: `amount_cap_per_role` is retained configuration, with no
  active reader elsewhere in the Python source. Import preserves it, including a
  negative integer; no new allocation cap is invented. Existing tier/role
  allocation remains owned by passallocation.
- `passes.py:2565-2601` defines a default-price helper with first-tier, event-price
  and legacy-map fallbacks. Its only current call path at `1394` already supplies
  an explicit price, which returns before those fallbacks. Actual omitted-price
  assignment at `1376` calls the tier resolver, which refuses no-tier events at
  `3553`. Preserve event price as configuration; both automatic queue and omitted-
  price admin assignment still require a tier. An explicit admin price remains
  supported. No new fallback assignment or sale tier is invented.
- `passes.py:4364-4416`: after queue recalculation, every registration lacking
  `sent_to_hype_thread` is considered, independently of booking state. The source
  writes the marker before sending, including when no destination is configured.
  Numeric channels remain numeric; source strings receive `@`. The configured
  topic and thread locale determine delivery. EN/RU text comes from
  `i18n/en/bot.ftl:460` and `i18n/ru/bot.ftl:496`; names use the accepted Core
  display-name mapping. Role is leader/follower, with the source RU grammatical
  form. HTML escaping names fixes source markup injection without changing plain
  visible text.

## Configuration import and representation

All declared EventSettings fields have explicit conversion. Unknown fields still
block; attestations do not bypass malformed values. Separate short and long titles,
country emoji, destination/topic/locale, default price and role-cap metadata survive
the same event transaction and replay comparison. Destination strings are converted
to the actual Telegram username representation. Channel values must be strings or
integers; NUL text and unrepresentable values are blocked. Whether the destination
exists or allows this bot to send is checked by Telegram at delivery, as in the
source. Numeric values must fit target types, without silent truncation.

Pass field `notified_no_more_passes` has no verified active source reader or target
disposition and is rejected as unmapped. It must not become an accepted opaque
configuration field merely because a prior allowlist contained its spelling.

Open-ended events carry `open_ended=true`; their existing non-null finish column
uses `9999-12-31T23:59:59.999999Z` as a compatibility sentinel. The marker preserves
the distinction from an explicit finite finish. Missing tier starts use the same
sentinel, corresponding to Python datetime.max. Explicit dates still require strict
lossless RFC3339 input and source-clock attestation. Empty or absent tier lists are
supported; explicit null is invalid. Original record bytes remain bound by the
durable source reference and private archive.

The migration is additive. Existing events retain previous titles/defaults. New
runtime requires migration058; old runtime can read the expanded schema. Rollback
means stopping the new runtime and reverting its binaries, retaining schema and
evidence. Do not drop announcement tables or source fields during rollback. An
old foundation plan is not a writable upgrade over an already imported event;
its existing receipt continues to prevent target overwrite.

## Durable announcements and imported history

Booking persistence enqueues a destination/name/role/locale snapshot in the same
transaction. Uniqueness is `(event_id,owner,created_at)`. Replaying commands or
recalculating does not generate duplicate items. An empty destination records
suppression, so configuring a destination later does not broadcast old registrations.

Pass plan v4 preserves `sent_to_hype_thread` **presence and raw value**. A present
null, false, timestamp or other JSON value has the same source presence semantics;
none is treated as absent and none is invented as a timestamp. Present markers
suppress delivery. For an absent marker, the bound pass resolution must explicitly
choose `historical_announcements`:

- `suppress_historical`: record suppression for imported registrations.
- `preserve_source_eligibility`: enqueue only active events with a configured
  destination; record suppression for the others.

No omitted policy defaults to sending. Immutable import metadata stores the source
marker and resolution separately from mutable delivery state, so delivery does
not alter the receipt's import evidence. Existing pre058 imported registrations
are suppressed by migration058 and retain their source references. A new registration
generation can be announced after an old imported generation was suppressed.

A service-authenticated claim marks `sending` durably before contacting Telegram.
Successful completion stores the returned message ID. Explicit rate limits retry
up to three attempts after the returned bounded cooldown. Rejections become failed;
ambiguous network outcomes and abandoned sends become unknown and are not
automatically resent. This deliberately improves source failure observability;
there is no exactly-once Telegram claim. A crash after claim but before send can
leave an unknown item without a delivered message. Unknown/failed states remain
inspectable durable records requiring an operator decision.

## Acceptance

Required: importer lossless roundtrip/drift refusal, independent marker presence
and policy tests, old users/pass preservation, EN/RU event title/country display,
numeric and username destinations/topics, source selection, no-tier refusal,
registration/recalculation/replay persistence, simultaneous claims, restart and
rate-limit/forbidden/unknown handling. Telegram-like QA uses an isolated stand and
fake Telegram only. Static tests do not replace that gate.

Written by events_codeqa (gpt-6-sol/Codex)
on behalf of Daniel Drizhuk

## Delivery service HTTP contract

These endpoints use the private delivery service credential, not a user access
 token. Send `Authorization: Bearer <token>`.

- `POST /internal/pass-announcements/claim` needs no request body. It returns
  `{found, announcement:{id,channel,thread_id,locale,name,role,attempts}}`.
  `thread_id` may be null. `found:false` means no currently eligible item.
- `POST /internal/pass-announcements/complete` accepts
  `{id,message_id,failure,retry_after}`. Success uses a positive `message_id` and
  empty `failure`. Failures use zero `message_id` and one of
  `telegram_rate_limit`, `telegram_rejected`, `telegram_outcome_unknown`.
  `retry_after` is seconds, 0–3600, for rate limiting. The third rate-limited
  attempt becomes failed. Completion requires the claimed sending state;
  stale completion returns conflict. Response is `{ok:true}` on success.

For an isolated synthetic stand, the delivery token format is two unpadded
base64url segments separated by a period: encoded JSON claims, then
HMAC-SHA256 over the encoded first segment using the configured signing key.
Claims are `{sub:"telegram-delivery",actor:"sandbox-bot",
aud:"zns-notifications",exp:<Unix seconds>}`; issue with a 60-second lifetime.
The actor string is the existing service protocol identifier even when runtime
user authentication uses Zitadel. Do not put this secret or token in browser
state, user-visible messages or operational logs. A normal `zns-core` user token
cannot claim or complete announcements.
