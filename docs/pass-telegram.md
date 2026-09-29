# Pass registration in Telegram

`/passes` discovers events through the pass service and opens a separate persisted
card. The menu offers solo registration, payment contacts, owner status, incoming
invitations, profile controls, and cancellation. Authorized booking administrators
also get the registration queue, cancellation, couple separation and allocation
refresh. Every mutation goes through the authenticated pass service.

Migration 032 stores owner-bound view state, card identifiers and callback actions.
Tokens include the owner, view revision and full action. A callback only resolves
for its owner and current revision. Commands carry current actor and target
versions plus a token-derived idempotency key. Stale controls refresh the view;
they never bypass service authorization or mutate another owner's UI state.
Successful card delivery prunes old callback records. Telegram message edits keep
the existing card when possible, and stored state survives process restarts.

Queue and invitation API pages contain 25 entries. Cards show five at a time.
Navigation stores an opaque API cursor and local offset; it clamps offsets after
data changes. The last 64 cursors support backwards navigation. Forward traversal
has no fixed limit, and “Back to first page” remains available after old cursor
history is discarded. These are live lists, not transaction snapshots: mutations
can move entries between refreshes. Cancelled items remain truthful where returned
by the service, and stale action versions are rejected there.

`/profile`, `/name`, `/legal_name`, `/passport` and `/role` reuse the existing owner
profile workflow. Name and passport prompts are hints for the agent, not a raw
text form that captures unrelated questions. Role buttons set an explicit value.
Profile buttons are separately persisted and bound to their owner and profile
version. Passport values are never included in these cards or callback actions.
Frozen identity fields have no editing buttons; role changes follow the existing
profile service policy. Both English and Russian are catalog-backed.

The event menu checks current profile requirements before offering solo/invite.
Missing required role, full name or passport data has localized guidance and
the relevant profile controls on that card. Identity fields are required only
for events whose domain configuration requires them. Concurrent profile changes
are still checked by the service. Failure notices are stored with the owner view,
so periodic refresh/restart cannot erase them; the next action/navigation clears
the notice. Profile guidance disappears once the current requirements are met.

The partner invitation menu supplies a contextual prompt. The on-demand
`registration` skill can read current event and registration data, then propose
the same service commands. Each interaction has at most three persisted reads;
an identical read is rejected without consuming another slot. Reservations survive
cancellation/restart so retries do not reset the budget. Events are projected in
pages of 20, queue/invitation API pages remain bounded at 25, individual results
have a 16 KiB cap and the registration model context has a 20 KiB cap. Truncated
context is explicitly marked and cannot supply omitted target versions.

The host binds versions, actor identity and idempotency keys. Invitation responses
must target an invitation returned to that actor; administrator changes require
an explicitly authorized queue read. Persisted queue results are hidden from a
retry after authorization is revoked. Payment contacts come from the event API,
and invitation partner IDs must match current explicit numeric text/voice or
trusted Telegram contact/visible-forward metadata. Names, usernames and historical
mentions are not resolved by guessing. The numeric check only grounds a model's
semantic proposal; it does not classify user intent or consume unrelated text.

Telegram's [Contact](https://core.telegram.org/bots/api#contact) supplies an optional
numeric `user_id`. A [visible forward](https://core.telegram.org/bots/api#messageoriginuser)
supplies `sender_user`; a hidden sender supplies no usable identity. Phone numbers
are not collected for this purpose. The local sandbox has synthetic contact,
visible-forward and hidden-sender controls; real mouse and touch browser tests
verify their wire payloads and rendered messages.

Agent replies use native Markdown conversion, while manual card text remains
literal. Replies bind to the menu revision; later manual navigation retires that
inline reply while the conversation archive preserves it. The
[administrative assignment adapter](pass-admin-telegram.md) uses separate global
booking-administrator rights and never infers them from payment administration.

Focused PostgreSQL and fake Telegram tests exercise both locales, foreign/stale
callbacks, solo cancellation, invitation acceptance, administrator couple
separation, profile controls, restart persistence, and traversal beyond one API
page. Independent browser Functional QA remains a separate acceptance gate.

## Pass receipts and review

The payment card shows the server's current total in RUB, including the actual
stored prices of both reciprocal partners. It does not double one participant's
price. A photo/document uses the shared neutral media intake. The model sees the
actual attachment and proposes receipt, avatar, clarification or another purpose.
A pending payment prompt never consumes an unrelated question.

Receipt candidates include unpaid orders and assigned passes. A unique observed
amount/currency may select a destination; equal totals across either domain need
a choice. A named current event or a natural reference to the delivered choices
can select a pass. Versions come from the delivered choice, never a freshly
reordered list. Buttons show up to 20 candidates per domain, so many orders do
not hide all passes. Amount matching still checks every authorized quote.
The event menu and explicit current event references remain available beyond
that button projection.

Migration 034 adds a separate typed registration command to the existing intake
and extends its button destination key. A database constraint prevents one intake
from holding both an order command and a registration command. First selection
is persisted before proof promotion and dispatch. Owner-bound immutable proof
storage is shared with orders. Retried dispatch uses the same domain key;
source expiry cannot discard a command whose immutable proof was already saved.
An authorization failure after dispatch is reported as an unknown outcome rather
than claiming that payment failed. Telegram document delivery itself can repeat
after an ambiguous transport acknowledgement; it does not repeat payment effects.

The domain's `paid` pass state means a receipt was submitted, not accepted.
Cards report the separate payment decision. Event payment administrators, including
hidden administrators, can open the payment review queue. It uses API pages of
25 and menu pages of five. Each shared couple attempt appears once. Accept/reject
buttons bind the actor version, participant version and immutable attempt.
File buttons recheck actor access and the displayed attempt/version before sending.
Free administrative assignments have accepted metadata and no receipt file button.

The registration skill can read `payment` and `payment_queue`, then propose
`proof_accept` or `proof_reject` against a returned target. The host binds all
versions and the attempt. Review reads share the existing three-read and byte
budgets and are reauthorized on retry. OCR never supplies review authority.
