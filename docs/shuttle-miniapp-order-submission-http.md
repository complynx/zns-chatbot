# Native order submission HTTP component

The candidate exposes native order preparation, explicit submission,
cancellation and read/recovery routes. It uses the shared reviewed store/session
transactions described in `shuttle-miniapp-order-submission-store.md`. The
RU/EN submission UI component is documented separately in
`shuttle-miniapp-order-submission-ui.md`; actual Telegram acceptance is still
required.

## Contract

All paths below start with `/app/orders/{event}`. Event identity comes only
from the path. Request bodies are flat JSON objects: do not spread the prepared
`arguments` object into the native body because it also contains `event_id`.
Unknown fields, trailing JSON and missing/nonpositive versions are rejected.

- `POST /submit/prepare`: `operation_id`, `expected_event_version`,
  `expected_catalog_version`, `expected_draft_version`.
Returns the immutable `OrderSubmissionResult`, including the original cleared
  selection.
Preparation alone does not submit an order.
- `POST /cancel`: `operation_id`, `order_id`, `expected_version`.
This explicit
  targeted owner request needs no separate approval flag.
Returns the cancelled
  order.
Current state, revision and frozen cutoff are checked by the store.
- `GET /submitted`: owner/event orders, optional nonzero UUID `after` and
  `limit` from 1 to 20, default 20.
The shared byte budget can shorten a page;
  follow its returned `next`, which names the last included order.
- `GET /submitted/{order}`: current state of this owner's exact order.
- `GET /operation-receipts/{kind}/{operation}`: exact immutable receipt, with
  kind `prepare`, `submit` or `cancel`.
Returns operation metadata and raw result.
A missing submit receipt is unknown, even if preparation exists.
Retry the
  original body/key; never infer non-execution from a missing status alone.

These native routes do not use the agent protocol envelope or its status-response
shape. They share proposals, canonical source-draft deduplication and business
receipts with the agent API. A native preparation can be completed through the
agent API and an agent preparation through native HTTP. Historical receipt and
current order/selection reads remain distinct.

## Access and effects

Requests use the existing origin, content-type, no-store and bearer protections.
Session/IAM authentication occurs before the store call; write transactions
recheck session expiry, generation and deployment binding under the shared lock
order. Reads and writes require current owner/event/module/assigned-pass access.
Waitlisted users and foreign identities cannot recover another owner's data.
Before serializing a successful response the server rechecks the current session.
If logout follows a committed write, delivery may fail; after signing in again,
recover the original operation rather than issuing a new write.

No lock is held over external IAM/Loom/Telegram calls. Submission confirms no
money and reserves no capacity. Cancellation has no money or capacity effect.
Trusted native action history is context for the next explicit user message;
native requests do not create proactive agent tasks.
