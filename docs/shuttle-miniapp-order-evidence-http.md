# Native owner order evidence API

## Routes

All routes require the existing Mini App bearer session. IAM credentials stay on
the server. The server derives owner and deployment from that session. Event UUID
comes from the path /app/orders/{event}.

- GET /proof-files: this owner's accepted attachment metadata, optional after UUID.
  Files are not event-bound before the user explicitly selects an order. The event
  still requires current orders entitlement. Each page has at most 20 files and
  next is the last included UUID. Follow it as after; UUID order is not recency.
- POST /payment-evidence: explicit nonzero operation_id, order_id, file_id,
  expected_order_version and amount_minor. Missing/null versions or amounts are
  rejected. Currency and timestamps come from the frozen order and stored Telegram
  provenance, never the browser. The normal body bound, strict JSON decoding and
  same-origin checks apply.
- GET /payment-evidence: current owner evidence history for the event, optionally
  filtered by order_id, with after cursor. At most 20 claims per page; includes
  human-reviewed history. This lets a fresh form session discover a claim submitted
  through the agent without carrying its evidence/operation UUID across interfaces.
- GET /payment-evidence/{evidence}: current owner claim and human decision.
- GET /payment-evidence-operations/{operation}: original submit receipt, including
  evidence as it was when admitted. This differs from current evidence after review.
  A missing receipt returns 404 and means unknown, not proof of non-execution.

File metadata contains opaque file UUID, kind, MIME type, filename, declared size,
Telegram sent_at and local accepted_at. It contains no Telegram locator or bytes.
Filename/MIME/kind previews are limited to 256/128/32 characters. A listed source
can become unavailable in Telegram; listing does not promise readable content or
classify a file as payment proof. The explicit selected file/order is retained as
a claim under the existing admission policy. Listing creates no claim or holds.

## Transaction and recovery

Submit and every native read use miniAppOrderTx: operation lock, event lock,
owner/session validation, then current assigned-pass/module entitlement. The
session is rechecked after event-lock waits, inside the same transaction that
would retain evidence, resource holds, notices and receipt. Logout, session expiry
or generation changes deny both new writes and original receipt replay.

Agent and native submission call the same submitOrderEvidence implementation and
hash exactly the same business arguments. Retrying the original operation and
body returns the original receipt after current authorization. Changing arguments
under that key returns conflict. A new operation is not a recovery strategy;
an existing non-rejected claim still prevents duplicate payment targeting.
Native reads and successful response delivery also recheck the active session.
A write may have committed before logout or a lost reply; sign in again and
recover with the original operation ID.

No write confirms money, fulfills an order or executes refunds. Exact timely
full-price claims can hold available capacity; unresolved or exhausted claims
retain their established policy states. Native submission does not relax the
partial/mixed/late policy boundary. An ambassador role does not grant access to
these owner routes for another user's files or orders.
