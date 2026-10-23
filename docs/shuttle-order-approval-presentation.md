# Complete order approval presentation

For `zinusya_orders.submit`, the task service reads the immutable prepare receipt
using the original operation ID and proposed event. Lookup checks current
owner/event/module/assigned-pass access under the existing deployment scope. The
recipient Telegram identity must match the task's owner. Arguments are strictly
decoded and must equal the stored preparation; Loom's deadline cannot extend its
expiry. The current draft is not used to replace the proposed snapshot. An
unavailable/mismatched receipt returns before saving an approval presentation,
so the same durable task retries rather than falling back to incomplete text.

The rendered RU/EN message contains every item and required-charge line, offer
and item/supplement IDs, quantity, unit/line amount, total, cutoff and expiry in
the event zone, and the no-capacity/no-money consequence. Integer decimal
formatting preserves exact minor units. Literal fenced text protects labels;
there is no raw argument JSON for this specialized order presentation. Other
approval methods retain their existing complete-parameter rendering.

Full text uses the existing durable Telegram chunk/outbox path, with consent
keyboard only after the last chunk. It is not squeezed into Loom's 4000-byte
preparation description; that API contract and manifest revision remain unchanged.
The original Approval object still supplies all pending-action keys, protocol
callbacks and presentation context. Only display text is passed separately.
Returning from an intervening question therefore restores the same approval
keyboard without changing the action hash or deduplicating its new presentation.

Distinct item IDs remain visible even when names/prices match in the same offer.
The native review/history renderer now includes these IDs too; its existing
transaction/recovery behavior is unchanged.

## Frozen service context

The frozen snapshot retains one service entry per selected offer: its ID,
EN/RU name and absolute UTC start/end. Both exact Telegram consent and native
review/order history render these saved values in the snapshot event timezone.
Catalog edits do not rewrite sold names or intervals. Missing service context in
historical orders remains absent; it is never reconstructed from today's catalog.
An unsubmitted legacy preparation fails closed on execution. A new preparation
operation creates updated terms requiring fresh consent; replay of the original
operation still returns its original receipt.

See [original operation recovery](shuttle-order-operation-adoption.md).
