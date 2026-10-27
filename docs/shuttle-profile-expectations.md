# User profile and name expectations

The independent owner profile stores separate first and last names, a derived
full name, and an interface language:
`auto`, `en`, or `ru`. Telegram intake continues to refresh Telegram metadata.
It does not copy a Telegram display name into the full name, overwrite a saved
name, or change the user's language choice. The full name is user-provided text,
not verified legal identity. Changes to original structured name components
schedule a name-only IAM update. Latin spelling changes do not. The agreed onboarding and durable name-sync requirements are recorded
in the [specification](shuttle-spec.md#агентские-ожидания-и-пользовательский-контекст).

The IAM adapter now provides `ReadProfileName` and `SyncProfileName` for a
persisted user binding.
It checks the exact subject, organization, verified
synthetic email and active account.
Its partial
[Zitadel update](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.UpdateUser)
contains only separate given and family names and requires matching read-back.
Populated IAM names do not establish user confirmation.
The adapter is not an agent endpoint.
The structured-name store and River
worker described below are implemented.
The agent and Mini App APIs now accept
separate name components.
First-use onboarding and `/name` use the agent dialogue; a verified existing
IAM name can be reused before that dialogue when the profile is eligible.

## Structured names and IAM synchronization

Migration 71 adds separate first and last names, name provenance, and one
durable desired-sync row per profile. Existing full-name strings remain intact;
the migration does not guess their components or mark onboarding complete.
The internal profile mutation accepts either legacy `full_name` or separate
`first_name` and `last_name`, never both. Separate names are trimmed, nonempty,
at most 200 characters each and at most 256 characters combined with their
separator. Control characters and malformed UTF-8 are rejected. Components can
contain spaces. A legacy write cannot erase confirmed structured components.

A structured name change saves the profile, immutable operation receipt and
latest desired IAM version in one transaction. Changing language, repeating
the same name or replaying an old receipt does not schedule another IAM write.
Both profile entry points share that transaction, including Mini App storage.
The agent and Mini App payloads require separate name components. The Mini App
shows two labeled inputs and never guesses a split for an older full-name string.
Its uncertain write retains both components and the same operation ID across
reload. A form change can complete an agent-created expectation; a later agent
correction is visible in the form. Replaying an older form receipt does not
overwrite that later correction or reschedule the old IAM name.

This is a breaking input change in `mvp-92`: `set_name` and `complete_name`
require `first_name` and `last_name` instead of `full_name`. Mini App
`POST /app/profile/name` uses the same fields. Name reads and historical
operation receipts still contain `full_name`; newer results also contain the
components and `name_source`. Existing records and receipts are not rewritten.

The existing service repair loop queues `zinusya_profile_name_sync` jobs with
only user ID and revision hints. The worker always reads the current desired
name. It holds the existing owner lock during bounded IAM dispatch, serializing
local edits and revocation with admitted writes. Success acknowledges only the
current desired revision after IAM read-back. A failure leaves durable work and
a 30-second backoff; an early River retry snoozes rather than completing the
job. Restart discovers unacknowledged work through the same repair loop.
Queue and worker metrics include the new bounded job kind. Once a name is
explicitly saved in the structured profile, that profile is authoritative for
IAM synchronization. The worker checks it again every ten minutes, including
after acknowledgement. An unchanged IAM value needs only a read. Completed
River jobs from an earlier minute cannot suppress this later verification.

This local lock cannot recall an HTTP request that IAM already received.
Timeouts and cancellation therefore remain uncertain remote outcomes and leave
the intent pending for reconciliation. Periodic checks also repair an older
request that finishes after a newer name was already acknowledged, including
when cancellation prevented recording the old attempt's error. This is eventual
reconciliation; the database lock is not a remote fencing token. No local
receipt claims remote success without a confirmed name. Legal registration
details are not changed.

The `zinusya_profile` module exposes `get`, `set_name`, `set_language`,
`request_name`, `complete_name`, `cancel_name`, and `operation`. Routes use
`POST /internal/agent/profile/{method}` and the existing agent envelope. Access
is always the authenticated local owner in the current bot and organization.
An administrator does not gain access to another user's profile or receipts.

## Original and Latin names

The profile keeps confirmed `first_name` and `last_name` separately from optional
`first_name_latin` and `last_name_latin`. The agent immediately transliterates
confirmed non-Latin components when saving a new name or correction. It does not
translate names or replace them with English equivalents. Explicit user or
passport Latin spelling takes priority; generated transliteration is not a claim
of official passport spelling. Names already in Latin script keep their spelling.
Older profiles are not automatically backfilled or asked to repeat onboarding.

The agent's `set_name`/`complete_name` and Mini App name endpoint accept the two
Latin fields together. Both empty explicitly clears them; omitting both preserves
them when the originals are unchanged and clears them when either original
component changes. Each nonempty Latin component contains Latin letters,
inherited combining marks, spaces, hyphens or apostrophes, with at least one
letter. Limits are 200 Unicode characters per component and 256 including their
separating space. The original name validation remains unchanged.

Original components, Latin pair, version and immutable receipt commit in one
owner-locked transaction. The writer saves originals first, then the Latin pair;
a compatibility trigger clears stale Latin if an older writer changes either
original component. Latin-only changes advance the version without changing the
original names or scheduling IAM synchronization. Confirming an IAM-imported
profile may change provenance to `user`; identical original components still
produce no remote name write. Legal registration fields remain separate.

Omitted Latin fields retain the previous normalized request bytes. Historical
operation receipts remain immutable and may omit Latin fields. An uncertain
write must retain its operation ID and exact original/Latin arguments; a later
correction must not rewrite an earlier receipt. The [schema compatibility guide](shuttle-schema.md)
describes the single version-74 migration for fresh and completed version-73
databases, including old-writer protection. No paid backfill is introduced.

## Version and retry contract

Read `get` before a write. A new profile has version zero, an empty full name,
and language `auto`. Writes require `expected_version`, a nonzero UUID
`operation_id`, and the same `idempotency_key`. Current Loom generates these
UUIDs. This module's operation-ID constraint is narrower than Loom's general
opaque-ID protocol. Tool argument schemas do not include a target user ID.

Writes atomically save their original result and a normalized request hash.
Retry the same operation with the same arguments after an uncertain response.
A matching receipt is returned before checking today's version or deadline.
Changing the arguments under that operation ID conflicts. A successful no-op
still gets a receipt and preserves the profile version. Current ownership,
subject, organization, bot and revocation are checked before returning receipts.

All writes advertise the `operation` status path. For status reconciliation,
send `protocol_version` and the original `operation_id` in the envelope.
The read tool also accepts `arguments.operation_id` to inspect a particular
write while retaining its own outer invocation ID. A missing receipt is
`not_found`; it is not proof that a previous network request cannot commit.
Storage failures remain retryable service errors. Stale versions, stale name
requests and changed retries are terminal rejections for that invocation.

## Name request lifecycle

There is one bounded full-name request per owner. It has a UUID, creation
instant and expiry instant. Repeating `request_name` while it is active returns
the same request without extending its deadline or increasing the version.
Expired requests are hidden from fresh reads. An explicit later request creates
a new UUID and deadline. No scheduler or proactive agent run is needed.

`complete_name` requires the current profile version, exact expectation ID,
and separate nonempty first and last names within the bounds above. The API
trims outer whitespace from each component. It checks expiry with database
wall time after acquiring the owner lock. Saving the name and clearing the
request happen in one transaction. `set_name` supports an explicit name change
without a pending request and clears any old request. `cancel_name` clears only
the matching active request. A language change preserves its ID and deadline
but advances the shared profile version when the preference changes.

Runtime setting `name_expectation_seconds` is optional. Zero or absence means
7200 seconds. Explicit values range from 60 to 86400 seconds. Change it through
the existing administrator settings CAS API. New requests use the setting;
existing deadlines do not move. `question_timeout_seconds` remains separate
from this setting and does not replace Loom question expiry.

## Agent context and language

Before each new durable Loom create request or textual answer/steering request,
the bot reads the current profile and active name request. It adds JSON marked
as untrusted data, a database observation instant, and instructions to classify
the message. Up to three earlier successful agent exchanges from the same user's chat
provide language evidence, newest first, with each text/reply capped at 512
characters. They are explicitly marked as historical data, not new requests or
instructions. This lets a neutral follow-up retain conversation language across
completed runs and application restarts. A successful agent result remains language
evidence when its delivery is unknown or failed, or its task was explicitly closed.
This does not retry that delivery. A read failure delays dispatch. Once the outgoing request is
checkpointed, retries preserve its exact text even if the profile changes.
Callbacks and approval decisions keep their existing structured payloads.

An expectation is a hint. An unrelated message does not set a name or clear the
request. An expired expectation does not consume the next message. The agent
can read the profile again and the API enforces UUID/version/deadline checks.
Name expiry never cancels a registration, changes a pass, or credits money.
Payment expectations remain derived from pass/evidence state and deadlines;
this slice does not add a generic workflow engine or duplicate financial state.

The explicit interface language controls system messages, pair notifications,
payment notifications and the fallback for language-neutral input. `auto` uses
Telegram language, then the configured default when Telegram has none.
`ru`, `be`, and `uk`, including regional variants, resolve to Russian; other
language codes resolve to English. The agent still answers meaningful user text
or captions in their language. For a name or attachment without linguistic text,
it uses previous conversation language, then the interface-language fallback.
Notifications freeze their text when reserving their durable outbound payload.

## Storage and verification

Migration 19 adds owner profile and operation-receipt tables. It does not backfill
names, alter Telegram metadata or change existing financial records. Rollback
refuses to drop populated profile/receipt tables. Export and retain these records
before any deliberate destructive rollback. Returning to an older application
image leaves the additive tables intact but does not expose the new tools.

Behavioral integration coverage includes name completion, no sliding deadline,
configured TTL, cancellation, stale CAS/ID, original receipt replay after restart
and later edits, Telegram metadata isolation, scoped authorization/revocation,
an unchanged unpaid pass after name expiry, exact manifest output validation,
fresh dispatch context, immutable uncertain-create retries, textual answer and
steering context, profile-storage outages and notification language choice.
Native owned-stand acceptance is recorded separately when complete.
