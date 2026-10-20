# Knowledge notice delivery resolution

Status: source mvp83, additive migration67. The primary remains schema65/mvp81.
The preceding mvp82 delivery acceptance is recorded separately in
[the runtime report](shuttle-knowledge-notices-runtime-qa-2026-10-20.md).
The separate [real-IAM/Loom runtime gate](shuttle-knowledge-resolution-runtime-qa-2026-10-20.md)
now passes direct diagnostics/resolution and a controlled free-text consent
dialogue through graceful restart. No main installation is claimed.

## Administrative workflow

`zinusya_knowledge.notifications` still lists bounded recipient-owned delivery
metadata; current global administrators can list deployment-wide metadata.
Ordinary users and named knowledge moderators do not receive administrative
retry/dismiss authority.

A current global administrator can call
`zinusya_administration.knowledge_notification` with the notification `id`.
It returns delivery state/version, recipient and suggestion locators, original
event origin when present, attempt ID and Telegram receipt. It excludes proposed
content and the raw outbound payload. Delivery is separate from moderation.

Use the existing `zinusya_administration.resolve_notification` tool with
`notification_kind=knowledge`, notification ID, observed version, action and
reason. Event-origin suggestions require their original `event_id`, including
after the event ends. Omit `event_id` for suggestions without an event origin.
Other notification families still require their event ID.

Preparation freezes the exact normalized arguments and an expiring proposal.
Confirm only with explicit exact-parameter consent, the same operation ID and
idempotency key, unchanged arguments and original proposal ID. Current global
administrator authority, deployment, original event and delivery version are
checked again. Altered, expired or stale proposals cannot change delivery.

- `retry` creates a new pending delivery generation on the same notice. The
  recipient must still be eligible, and the worker rechecks IAM and local access
  before sending. A proposal that becomes superseded or loses its reviewer is
  not a reason to repeat the original business operation.
- `dismiss` closes delivery without proving that Telegram received it. It can
  close an inaccessible or superseded recipient record. It changes no suggestion,
  article, publication, review or payment state.

Only unknown, failed or blocked notices can be resolved. Pending, inflight,
delivered and already dismissed notices cannot be changed by a new resolution.
For unknown delivery, inspect the receipt and chat first. An explicit retry may
produce a duplicate and is a last resort; it is never automatic.

After a lost response, call `status` or `reconcile` with the original operation
ID. Reconfirming the original unchanged operation returns the immutable original
resolution receipt, even after proposal expiry or subsequent delivery. That
receipt does not replace the current notice state. Never create a replacement
operation merely because an observation timed out.

## Storage and rollback

Migration67 extends the existing resolution table with a foreign key to a
knowledge notice and retains its exactly-one-notification-family constraint.
It adds the terminal `dismissed` knowledge delivery state. It does not rewrite
suggestions, articles or other notification families.

The down migration refuses to discard knowledge resolution history or dismissed
notices. Export and decide any intentional history removal separately. Preceding
binary compatibility has a separate linked report; runtime installation still
requires its own proof.

## Focused verification

The full knowledge and existing notification-resolution Linux/PostgreSQL race
selection passes 34 roots and 63 terminal cases, with no failures, skips or OOM.
It applies migration67 and uses mocked IAM, Loom and Telegram HTTP peers. It
is not a full real-IAM/Loom runtime gate. The new behavioral scenarios cover
HTTP prepare/confirm, explicit retry or dismissal, current administrative
access, metadata redaction, original receipt replay after expiry and later
delivery, changed/stale proposals, revoked recipients and retained event origin
after the event ends. Existing payment, pair, order and massage resolution
families are included in the same successful selection.

All affected API, DB, task and observability package tests pass. Pinned sqlc
compiles and regenerates without a diff. The Linux amd64 binary builds with
CGO disabled. Source correctness, quality and final acceptance reviews found
no actionable findings.

The first two PostgreSQL selections each retained 33 passing roots and one
failing new HTTP test. The first test sent confirmation fields to read/prepare
schemas; the second compared JSONB/HTTP receipt whitespace instead of values.
Both test assertions were corrected. Neither original failure was relabeled as
a pass; their logs and decoded database dumps remain separate. A broad formatter
also touched archived dependency copies; those incidental changes were fully
restored. Final test line endings were normalized only after successful race
cleanup, with a recorded byte-equivalence check excluding CRLF/LF differences.

All three temporary PostgreSQL/test-container projects are archived and removed.
The primary stand remains unchanged. Ignored proof lives under
`.cache/shuttle-work/knowledge-notice-resolutions-source-v1/`,
`knowledge-notice-resolutions-pg-v1/`, `knowledge-notice-resolutions-pg-v2/`,
`knowledge-notice-resolutions-pg-v3/` and `knowledge-notice-resolutions-build-v1/`.
The linked full runtime gate now passes with a controlled provider. The [mvp82 binary compatibility gate](shuttle-knowledge-resolution-compat-qa-2026-10-20.md)
also passes83/82/83 and, in a separate fresh fixture,83/81/83 against populated
schema67. Main installation and autonomous model quality remain separate
acceptance work. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
