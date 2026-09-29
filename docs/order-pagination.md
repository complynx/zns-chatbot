# Telegram order pagination

The Telegram order view displays at most **10 customer order cards** and
**10 payment-review cards** per render. These are separate views with separate
positions. Payment instruction cards for the visible customer orders remain
available. The Core API still returns the complete collection through its
existing API pagination; no order is hidden from authorized data access.

Lists with ten or fewer items keep their existing layout until a pager has been
opened. Larger lists add an order or payment-review pager with the current page,
page count, total item count, and applicable Previous/Next buttons. After a list
shrinks, an existing pager remains and reports the current count. Empty lists use
page 1/1 with zero items and no navigation buttons.

## State and permissions

Migration `024_order_paging.sql` adds `bot.order_pages`,
`bot.order_page_buttons`, and a `visible` flag on `bot.order_cards`.

- A page is persisted by owner and scope (`orders` or `admin`). The first page is
  also the default when no row exists. Restarts preserve later page selections.
- Navigation uses `o:page:<token>` callbacks backed by a separate navigation
  table. They do not construct or execute an order mutation command.
- Tokens are looked up with the callback sender's owner identity. Foreign and
  unknown tokens cannot change page state. Existing Telegram private-chat sender
  checks still apply.
- Payment-review navigation rechecks the Core API permission. If that permission
  is revoked, its cards and navigation controls are retired.
- Navigation stores the requested absolute page. Duplicate updates do not
  advance it twice; an older update cannot rewind a newer selection.
- Every render clamps the position to the current collection. Removed orders,
  accepted payments, and empty lists cannot leave an out-of-range pager.
- A successful order action focuses the page containing the returned order when
  that order is still in the actor's authorized list. This also applies when an
  agent explicitly names an off-page order. Core validation and idempotency remain
  unchanged.

## Historical cards

When a card leaves the visible page, its Telegram buttons are removed and its
text becomes a neutral notice that the card is outside the current page. It does
not claim that its order was deleted, paid, or still exists. Cards whose removal
is confirmed by the current API collection retain the existing unavailable or
removed behavior. Returning to a page refreshes the original cards with the
current authoritative state.

Retired cards have `visible=false` and are excluded from subsequent retirement
work. Navigation tokens are stable for each owner, scope, and target page, so
clicking backward and forward does not create new token rows each time. Telegram
message records remain available for history and reuse.

This bounds normal page transitions to the current and previous visible cards.
An existing deployment that already sent a large unpaginated set may need to
retire that larger set once after migration. Clearing those already-published
Telegram controls requires individual edits. No sandbox or deployed database was
migrated as part of the implementation task.

## Verification

Real PostgreSQL integration tests cover page size, first/last navigation,
persistence across Bot reconstruction, duplicate/older callbacks, forged and
foreign-owner callbacks, read-only navigation, shrinking and empty collections,
retired controls, stable token counts, no repeated historical-card edits, admin
scope separation, permission revocation, and an agent mutation of an explicitly
named off-page order.

The existing 320-order regression keeps its 30-second deadline for processing
the large-list request followed by another user's update. It additionally checks
that only ten customer order cards are active, that all 320 orders remain
available through the API, and that an explicit mutation of order 320 displays
its updated card on page 32.

The focused PostgreSQL tests and pinned bot/integration lint passed. The native
Windows tool environment did not provide CGO for `go test -race`; the shared
Docker race gate is a separate required check. Browser Functional QA and
independent Code QA remain separate acceptance gates.
