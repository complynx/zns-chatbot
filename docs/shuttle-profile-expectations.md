# User profile and name expectations

The independent owner profile stores a full name and an interface language:
`auto`, `en`, or `ru`. Telegram intake continues to refresh Telegram metadata.
It does not copy a Telegram display name into the full name, overwrite a saved
name, or change the user's language choice. The full name is user-provided text,
not verified legal identity. It does not rename an IAM account.

The `zinusya_profile` module exposes `get`, `set_name`, `set_language`,
`request_name`, `complete_name`, `cancel_name`, and `operation`. Routes use
`POST /internal/agent/profile/{method}` and the existing agent envelope. Access
is always the authenticated local owner in the current bot and organization.
An administrator does not gain access to another user's profile or receipts.

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
and a nonempty full name of at most 256 Unicode characters, with no control
characters. The API trims outer whitespace. It checks expiry with database
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

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
