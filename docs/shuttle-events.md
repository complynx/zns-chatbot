# Event catalogue: first registration prerequisite

Migration 00003 adds event configuration and immutable operation receipts. It does
not change existing users, tasks, deliveries or task resolutions. The release
image embeds Go's IANA zone data; the scratch image does not need host zone files.

An event has an explicit UUID, name, IANA timezone, starts_at and ends_at absolute
instants, enabled module IDs and a positive version. API timestamps require
RFC3339 offsets. Submicrosecond instants are rejected because PostgreSQL stores
microseconds. Inputs without an offset are rejected, rather than interpreted
in the server zone. Instants are normalized to UTC for persistence, while the
named event zone determines display and DST. Local deadline parsing and conversion
of legacy naive dates remain separate work.

Module IDs are unique lowercase identifiers; the stored list is sorted. Event
configuration alone never grants a person access to a module. Assigned-pass
access, registration eligibility and ownership are described in the
[registration rules](shuttle-registration-rules.md) and
[event-context contract](shuttle-event-context.md). The original catalogue
checkpoint below did not establish that later access behavior.

## Administrative HTTP API

All routes use the private listener, verified user identity, local active account
and the API-project zinusya.admin role. Catalogue scope is the configured bot and
organization. These writes are operator APIs, not agent write tools.

- GET /internal/admin/events?limit=50&after=UUID: bounded 1..100 entries in stable
  UUID order; follow next with after. This is a live list, not a frozen snapshot.
- GET /internal/admin/events/{id}: current configuration, foreign scope returns 404.
- PUT /internal/admin/events/{id}: operation_id, expected_version and document.
  Use expected_version=0 to create that explicit ID; use the current version to
  replace configuration. A stale version or mismatched operation replay is 409.
- GET /internal/admin/event-operations/{operation_id}: the original successful
  result, scoped to the same operator, bot and organization. Unknown operations
  return 404; they are not an instruction to retry an unknown write with a new key.

Example document:

```json
{
  "operation_id": "create-grodno-fixture",
  "expected_version": 0,
  "document": {
    "name": "Grodno fixture",
    "timezone": "Europe/Minsk",
    "starts_at": "2026-11-10T15:00:00+03:00",
    "ends_at": "2026-11-12T15:00:00+03:00",
    "modules": ["orders", "massage"]
  }
}
```

A single transaction commits the event change and receipt. Concurrent duplicates
of the same normalized request share the original result. The request digest
includes the target ID, expected version and normalized document. Different
operators have separate operation namespaces. A replay returns its original
version even after later event edits. Failed validation, stale writes and rolled
back transactions have no successful receipt. Successful receipts retain actor,
request digest, full resulting configuration and timestamp for audit.

## Migration and rollback

Apply the explicit Shuttle migration command before starting this version. Old
binaries do not read these tables; messaging data remains compatible. To roll back
the executable, leave the additive tables and receipts in place. Do not run down
migration against populated event data as routine rollback: it drops the catalogue
and audit receipts. Export and reconcile configuration first if an explicit schema
rollback is required. No automatic production migration is performed here.

Behavioral acceptance covers create/update, duplicate and mismatched replay,
status lookup after a lost response, restart, concurrent retries/edits, bot scope,
role denial, invalid or naive timestamps, and local revocation. Time-zone checks
cover Moscow, Minsk, Ekaterinburg and the Berlin DST transition.

## Agent configuration writes (migration 31)

The separate `zinusya_administration.change_event` tool is implemented after the
operator catalogue API above. Its current verification status is tracked in
[the implementation plan](shuttle-event-change-plan.md). Both existing agent
catalogue reads remain available. Every agent configuration call requires the
current global `zinusya.admin` role and an exact active local account in this
bot/organization. An event administrator role alone is insufficient.

Arguments contain the explicit event `id`, `expected_version` and complete
`document`, including optional registration and deposit policies. Version zero
creates; a positive version replaces by compare-and-swap. Normalize before
hashing. Omitted/null registration or deposit removes that future policy, and
omitted modules are disabled. These changes do not edit frozen pass terms,
registrations, allocation, event authorities, payment rows or prior receipts.

The private agent routes are POST
`/internal/agent/administration/change_event/prepare`, the same base path for
confirmation, and `/status` or `/reconcile` for recovery. Preparation is effect
free and persists a five-minute proposal keyed by bot, organization, actor and
operation ID. Repeat preparation returns the same normalized arguments,
proposal, description and expiry. Changed arguments conflict. Confirmation
requires `exact_parameters`, the proposal ID and matching operation/idempotency
keys. Database expiry is checked after event-row or competing-create waits,
while the transaction can still roll back. Event, child policies and original
agent receipt commit atomically.

The agent proposal namespace is separate from operator event receipts. Even an
identical operator operation ID cannot satisfy agent confirmation. Successful
replay returns the original event version after later edits and expiry, subject
to current authorization. A lost response requires status/reconcile and the
same operation; a missing receipt is not permission to invent a replacement
write. Other authorized administrators cannot adopt another actor's proposal.

Prepare/confirm bodies are limited to 128 KiB, tool results to 256 KiB; status
and reconcile retain the existing 8 KiB request limit. The actual Loom broker
also bounds argument and result values. Its preparation description has a
separate 4,000-byte limit. The description contains a bounded summary, while the arguments
carry the entire normalized configuration. Telegram sends all arguments as
literal JSON across ordered message chunks, with approval buttons only on the
last chunk. The full-parameter presentation versions deduplication and callback
keys. Old summary buttons cannot approve a known-unapplied operation after an
upgrade; unknown outcomes retain their original idempotent reconciliation.

Migration 31 adds `shuttle_event_change_proposals`. Rolling back the executable
keeps this additive history. Its down migration refuses to discard any proposal,
including an expired or failed preparation. Export and reconcile history before
an explicit schema rollback. A compensating configuration change is a newly
approved versioned write, not deletion of the original receipt.
