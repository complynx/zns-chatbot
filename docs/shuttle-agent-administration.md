# Agent administration

The `zinusya_administration` Loom module exposes diagnostic reads:

- `settings`: current versioned runtime settings, excluding secrets.
- `tasks`: task metadata for this bot and organization, with optional state,
  UUID cursor `after`, and limit 1..20 (default 20). Follow `next` as `after`.
- `task`: diagnostics for an explicit task UUID, including queue, deliveries
  and local operator decisions. Message bodies, checkpoints and credentials
  remain excluded.

It also exposes `events`, `event` and `registrations` scoped reads, plus the
`assign` write with exact-parameter confirmation. See
[assignment protocol and current acceptance boundary](shuttle-agent-assignments.md).
Later business slices also add scoped authority, notification, orders, massage
and historical-archive methods to administrator discovery. Their individual
contracts and acceptance boundaries are linked from the [roadmap](shuttle-roadmap.md).

Order notification diagnostics now expose `order_notifications` and
`order_notification`. `resolve_notification` also accepts `notification_kind=order`
with exact preparation, original receipts and current global administrator checks.
These delivery operations do not repeat the order or change payment/capacity state.
See [the contract and current acceptance boundary](shuttle-order-notice-delivery.md).

Massage history now exposes `massage_bookings`, requiring `event_id` and accepting
`after` plus `limit` 1..20. It returns bookings and cancellations across owners
for one deployment event under global administrator authorization. Disabled
modules and missing passes do not hide retained history. Follow `next_after`
until absent, including after a short byte-bounded page. This does not grant
specialist permissions. See [contract and real-service acceptance](shuttle-massage-administration-qa-2026-10-17.md).

The static manifest requires the `zinusya.admin` Loom permission for this module.
Configure Loom's `identity.role_permissions` to map the existing Zitadel role
`zinusya.admin` to the permission `zinusya.admin`. Assign the role in both the
Loom project (for broker filtering) and the Shuttle API project (for API
authorization). The token must include both audiences. A mapping alone does not
grant a user a role.
The isolated test generator includes this mapping; production configuration needs
its own reviewed mapping. No caller permission or service privilege is expanded.

Discovery uses POST `/internal/agent/administration/discover` with the delegated
user bearer token and `{}`. It returns precisely `{"methods":[...]}`; Huma's schema
link field is omitted because Loom rejects extra discovery fields. Discovery is
not cached by Loom. A locally active user without the API project's administrator
role receives an empty methods array. Invalid identities, unavailable IAM and
locally revoked or foreign accounts retain their authorization errors.

Each advertised read method accepts POST at `/internal/agent/administration/{method}`
with Loom's `protocol_version`, `operation_id`, optional `run_id`, and `arguments`.
Every call independently verifies the administrator role and local account, even
if discovery previously succeeded. Responses echo the operation ID with status
`succeeded` and `result`. These reads share the existing operator handlers and
bot/organization scoping. They create no task resolution or business mutation.

Input bodies are limited to 8 KiB. Diagnostic reads advertise a five-second timeout
and 64 KiB result limit; the archive-validation reads override the timeout to
30 seconds. Use the advertised method contract for its specific budget.
An unusually large diagnostics result may exceed the broker
limit and must be inspected through the operator API. Lists have stable UUID
order, not chronological order or a transactionally frozen snapshot.

Agent `close_task` is implemented with durable preparation, exact confirmation and
actor-scoped status/reconcile. Behavioral tests and bounded native approval passed;
see the [task-closure contract](shuttle-task-closures.md) and
[acceptance record](shuttle-task-closure-qa-2026-10-15.md). The operator closure HTTP
API remains available. Agent [`update_settings`](shuttle-settings-changes.md) now
provides application-wide settings replacement with its own exact proposal,
original receipt and current-state IAM cache synchronization. Agent
[`revoke_user`](shuttle-user-revocations.md), a narrow `user` snapshot read and
read-only `user_revocation_receipt` diagnostic are implemented in source with
exact preparation and durable original receipts. Focused behavioral checks and
source reviews passed, followed by all 221 integration race scenarios, other Go
packages and strict lint. Native exact approval, self-revocation owner denial,
other-administrator diagnosis and restart/binary rollback passed; see the
[acceptance limits](shuttle-user-revocation-qa-2026-10-15.md).
The operator revocation HTTP API remains available.

Behavioral checks use real PostgreSQL and HTTP IAM/Loom/Telegram peers. They cover
role denial, method discovery, settings, bounded task lists, invalid UUIDs,
foreign task hiding, redaction, and local revocation after successful discovery.
Historical acceptance of the original three diagnostic reads passed on image
`sha256:d44e5fc04a017b4a3a6b3b345d069d29758dd8be94cbb18c3988ccfd380aa534`.
An isolated fixture administrator discovered all three methods and invoked
`modules.zinusya_administration.settings({})` through the real Loom script broker;
run `f4c4f6b7-961c-4161-9752-e6f8ba0961b8` completed with a validated result.
The first fixture attempt lacked the Loom-project role and was not a pass.
A non-admin Telegram request then called `account.me` successfully while the
administration module stayed hidden. Task `8fa1c88c-d80a-44b0-96bc-2266656e98ca`
and run `690fbd45-71d0-4975-97c6-53e7b69d3092` completed with one delivery.
Mailbox responses were QA-authored scenario inference, not an unattended model.
Full Windows tests, Linux race tests, lint and one independent textual review
passed with no actionable findings. The staged baseline remained empty.

Written by Codex (gpt-6/Codex desktop), Shuttle developer
on behalf of Daniel Drizhuk
