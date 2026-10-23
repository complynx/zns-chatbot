# Solo registration and bounded pass assignment

This slice implements owner registration to a waitlist, administrator pass assignment,
owner reads, operation receipts and the assigned-pass module eligibility predicate.
It does not implement pair invitations, automatic allocation, role changes,
cancellation, payment confirmation or Telegram menus.
Administrator agent confirmation and context resolution remain next.

## Configuration and lifecycle

An event must enable `registrations` and contain the explicit rules documented in
[registration configuration](shuttle-registration-rules.md). Registration and
assignment require the database clock to be strictly before `closes_at`.
A registration has one owner and one role: `leader` or `follower`. Repeating the
same role returns the existing registration and preserves its request time.
Changing role through this endpoint returns a conflict. Registration is unique
per event and user. It initially enters `waitlisted`; waiting alone grants no
module access.

An administrator assigns a waitlisted registration with its expected version and
the expected event version. The transaction locks the event, validates the current
configuration, checks capacity for the registration's role and creates a pass.
Concurrent assignments cannot exceed that capacity. The pass freezes price in
integer minor units, currency, currency scale, assignment time and payment due
instant. The due instant is the earlier of assignment plus the configured payment
window and sales closing. Event price edits do not change existing passes.

The registration becomes `assigned`. No payment is implied by assignment, including
for a zero-price pass. There is no automatic cancellation or forfeiture on expiry.
Payment recording and ambassador confirmation belong to the next roadmap slice.
An assigned pass, including an unpaid pass, qualifies its owner for a module enabled
in that event. A revoked account does not qualify. The availability predicate is a
read; consuming module mutations must check the entitlement in their own execution
transaction when those modules are implemented.

## Authenticated internal API

All routes require the current user's delegated bearer token and an active local
account in this bot and organization. Owner IDs derive from the token; an owner
cannot choose another account. Administrator routes require `zinusya.admin`.

- `POST /internal/events/{id}/registration`: `operation_id`, `role`.
- `GET /internal/events/{id}/registration`: current owner's registration and pass.
- `GET /internal/admin/events/{id}/registrations`: bounded operator list; optional
  `after` UUID cursor and `limit` 1–100, default 50. UUID paging is not waitlist order.
  The response includes `registrations` and, for a full page, `next`.
- `POST /internal/admin/events/{id}/registrations/{registration_id}/assign`:
  `operation_id`, `expected_version`, `expected_event_version`.
- `GET /internal/registration-operations/{operation_id}`: the calling actor's
  original result. Other actors cannot inspect that receipt.

A response is a registration object with UUIDs, role, state, version and UTC
`requested_at`; an assigned registration includes `pass`. Convert these instants
using the event's IANA timezone for presentation.

Example registration body:

```json
{"operation_id":"join-unique-request","role":"leader"}
```

Example assignment body, after the operator reads the event and registration:

```json
{"operation_id":"assign-unique-request","expected_version":1,"expected_event_version":1}
```

The operation key is scoped by bot, organization and actor. Its normalized request
hash includes action, event, recipient or registration, role and expected versions.
The effect and original receipt commit together. Exact retries return the original
result even after assignment, closing or later configuration changes. Reusing a
key with changed arguments conflicts. Use a distinct key only for a distinct
user action; after transport uncertainty, look up or retry the same key.
Receipts have no automatic retention deletion.

HTTP 403 denotes missing role or denied account, 404 an inaccessible or missing
record, 409 changed versions/arguments, full capacity, closed sales or disabled
registration. Storage failure returns 503. Do not treat a generic transport error
as proof that an effect did not commit.

## Migration and rollback

Migration `00006_registrations.sql` adds registrations, pass snapshots and operation
receipts without changing previous tables. Rolling back the application binary
leaves these data intact, but the previous binary has no registration API. Do not
run the destructive down migration merely to roll back a binary. Intentional data
rollback requires exporting all three tables and restoring their foreign-key
relationships and receipts.
