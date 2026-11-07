# Knowledge notice delivery resolution

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
