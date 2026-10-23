Current source also adds [native start and avatar entry](shuttle-native-start-avatar.md).

# Structured Telegram navigation

This document records the initial navigation slice.
Later work adds native
[registration and required details](shuttle-registration-details-native-entry.md),
[conversational questions](shuttle-agent-reply-keyboards.md) and the
[orders draft form entry](shuttle-orders-selection.md#native-entry).

`/menu`, `/profile` and `/registrations` are durable owner read/navigation commands.
They use the existing intake, authorization, queue and outbox without creating an
agent session or calling Loom. A pending agent question remains available after
navigation. This is the first foundation for the required complete structured,
agent and mixed workflows; it does not yet implement registration/name writes,
Mini App forms, pair actions or reply keyboards for agent questions/approvals.

## Routing and recovery

Normalization recognizes only these exact commands, with an optional canonical
UUID page cursor for `/registrations`. Attachments and unsupported media cannot
be reclassified as commands. Ordinary free text retains its agent routing; a name
expectation does not consume it as a structured name answer.

A structured task can be routed ahead of earlier unrouted messages. The short
conversation lock still protects routing, and its active task ID stays unchanged.
The routing watermark advances only through a contiguous prefix of routed tasks.
When an earlier ordinary message is routed later, it advances across the already
routed navigation tasks; ordinary messages retain their relative routing order.
No additional workflow engine, worker, database table or migration was introduced.
The additional query is kept in the sqlc source and generated code.

Reads bootstrap the existing canonical IAM account if needed, obtain the normal
delegated user credential and validate subject/organization before owner storage
reads. The identity client validates the exchange actor. IAM failures keep the
accepted task retryable; local revoke denies access. Outbox delivery performs its
existing current-authorization checks. Loom availability is not a prerequisite.

The response and completed task checkpoint commit together, with per-task outbox
keys. Restart reuses the stored response and its concrete keyboard type. The
existing uncertain-send policy applies: a lost Telegram send receipt does not
permit blind retry or establish exactly-once delivery.

## Views and keyboards

The menu, profile and final registration page use `ReplyKeyboardMarkup`.
Read-only pagination uses a localized inline Next page button with a compact
owner-scoped cursor. It grants no authority and is never an approval. Legacy typed
page-cursor commands remain readable for already sent keyboards. The Go sendMessage model admits only typed
inline, reply, removal and ForceReply variants; decoding a stored payload restores the
concrete variant. Old inline outbox payloads remain supported by the new code.
Agent questions and approvals still use their earlier inline presentation until
the separate durable question/operation binding and reply-keyboard stage is done.

Profile reads the saved name and pending expectation without changing either.
Registration status uses the same owner account read as Mini App/agent tools,
with bounded pagination, event-local times and module availability. Pass amounts
are confirmed ledger values; unreviewed payment evidence is not confirmed money.
The initial deposit amount/deadline is separate from total remaining and final
payment deadline. Disabled registration modules suppress pass details. User/event
names are plain text, never interpreted as Markdown. Interface text uses the
existing saved-language and Telegram-language fallback rules.

The next actual user-triggered agent create/control request includes up to five
prior completed navigation request facts from the same owner/conversation. Only
command and acceptance time were included in the accepted navigation image, as
untrusted history that proves neither
consent nor delivery. Existing create/control checkpointing freezes this context
for an uncertain retry. Recording navigation starts no proactive agent turn.
The newer working tree also includes completed Telegram profile operations with
a matching immutable receipt and profile_version; see
[direct Telegram name entry](shuttle-telegram-name-entry.md). These changes are
not yet in the deployed navigation image. Mini App write-action facts still need
their own transactional context integration.

## Application rollback boundary

The new binary reads legacy inline keyboards. The previous binary cannot handle
new structured routes or restore reply-keyboard outbox payloads. Before returning
to the previous image, stop ingress and drain/reconcile every structured task and
its undelivered outbox; do not strand accepted work by treating old-image startup
as proof of reverse compatibility. Preserve durable records and uncertain-send
receipts. Schema stays33. No schema rollback, production deployment, commit or
push is part of this slice.
