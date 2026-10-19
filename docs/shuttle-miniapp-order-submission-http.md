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
  `expected_catalog_version`, `expected_draft_version`. Returns the shared
  `OrderPreparation`: proposal ID, arguments, exact snapshot and expiry.
- `POST /submit`: the same fields, plus `proposal_id` and `confirmed: true`.
  Returns the immutable `OrderSubmissionResult`, including the original cleared
  selection. Preparation alone does not submit an order.
- `POST /cancel`: `operation_id`, `order_id`, `expected_version`. This explicit
  targeted owner request needs no separate approval flag. Returns the cancelled
  order. Current state, revision and frozen cutoff are checked by the store.
- `GET /submitted`: owner/event orders, optional nonzero UUID `after` and
  `limit` from 1 to 20, default 20. The shared byte budget can shorten a page;
  follow its returned `next`, which names the last included order.
- `GET /submitted/{order}`: current state of this owner's exact order.
- `GET /operation-receipts/{kind}/{operation}`: exact immutable receipt, with
  kind `prepare`, `submit` or `cancel`. Returns operation metadata and raw result.
  A missing submit receipt is unknown, even if preparation exists. Retry the
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

## Verification and remaining work

The broad Linux/race gate passed 49 integration roots/56 test events plus all
13 API roots/46 events: 62 roots/102 events, zero failures/skips/OOM. It used
real disposable PostgreSQL and mock IAM/Loom/Telegram. Source hashes for 873
inputs remained unchanged during this gate. The HTTP handoff test uses HTTP
on both sides, restarts the application, recovers the original receipt, then
closes mock Loom and submits, paginates, reads and cancels natively. Other cases
cover strict bodies/approval, origin, logout, unknown phases, changed bodies,
stale cancellation, foreign ownership and disabled modules.

A quality review identified a test assertion gap: matching cancellation retries
and GET results did not independently prove the state transition. The test now
asserts the exact order ID, `cancelled` state and revision increment. Only that
test file changed after the broad gate; all three native HTTP roots passed again
with Linux/race. Production source hashes remained identical. This was a focused
strengthening repeat, not another 62-root run on the final test file. Scoped
API/integration lint passed before that assertion addition.

Independent security and quality reviews found no production-code defect. A
fresh final textual review found no actionable findings. Its requested payment
comparison context path was incorrect and unavailable; this qualification is
retained. Reviewers ran no commands or tests.

Evidence: `.cache/shuttle-work/orders-native-http-v1-qa/` and
`.cache/shuttle-work/orders-native-http-proof-v2-qa/`, including exact roots,
source manifests/delta and cleanup receipts. Both exact QA pairs and dedicated
volumes were removed. The first PostgreSQL shutdown checkpoint exceeded the
helper's 30-second observation window; the same process later exited zero,
without restart or OOM. That cleanup timeout is retained in its checkpoint.
All ten main services remained unchanged.

At this HTTP component gate, the main stand remained schema39/manifest `mvp-39`; these source
changes target schema40/`mvp-40`. No commit, push, PR or production deployment
was performed. The index remains empty. This component gate does not establish
real IAM/Loom/Telegram or production acceptance. UI behavior/recovery has its own
component gate in `shuttle-miniapp-order-submission-ui.md`. Still required:
reviewed owned-stand activation, actual native-only with Loom down, agent-only and both
mixed Telegram paths, cancellation and service-restart recovery. Order proof,
capacity, payment review and meal fulfillment remain later required stages.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
