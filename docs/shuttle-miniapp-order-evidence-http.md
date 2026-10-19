# Native owner order evidence API

Current source integration: native owner/reviewer forms, reactive action history
and [durable order notices](shuttle-order-notice-delivery.md) now have separate
component verification. References to these as next work below record this page's
earlier stage. Owned activation and real native/agent/mixed/fault acceptance remain
pending. D23 money-policy questions remain open.

Source schema 42 / manifest mvp-43. This component adds owner Mini App HTTP access
to [order evidence](shuttle-order-evidence-mutations.md). It uses existing sessions,
order entitlement, admission and immutable receipts; no schema or module revision
is added. The owned main remains schema 40 / manifest mvp-40 and is not upgraded
by this component. Owner UI and proof-content controls are documented in
[the next component](shuttle-miniapp-order-proof-ui.md).
[Reviewer read routes](shuttle-miniapp-order-review-reads.md) are also available in
source, alongside [reviewer writes](shuttle-miniapp-order-review-writes.md). At this HTTP checkpoint, native
reviewer UI, reactive action context and notice delivery remained next; their
subsequent source verification is linked above. Real-service acceptance is still pending.

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

## Verification and limits

The ignored .cache/shuttle-work/miniapp-order-evidence-v2-qa/ gate passed 73 top-level
tests / 135 pass events: 60 integration roots / 89 events and 13 API roots / 46 events.
All 912 frozen inputs matched before/after. No test failed or skipped; no OOM
occurred. Scoped db/API/integration lint passed with zero issues.

Five new behavioral roots cover original receipt sharing with agent calls and
reopened-store recovery, logout during an observed PostgreSQL event-lock wait
with no evidence/holds/notices/receipt effect, owner-only file pagination, native
HTTP submission/current/original reads and exact-key retries, changed/missing
arguments and cross-origin rejection, plus agent-first discovery from a fresh
Mini App session. Existing order, shared review, authorization, file-race and
rollback scenarios also passed. HTTP tests stop background workers while leaving
the test server available; no Loom task is created by native submission.

An initial quality review found that native reads required an evidence/operation
UUID that a fresh form did not know. The bounded owner event/order list fixes this
handoff gap; the fresh-session behavior above now passes. Initial correctness and
fresh final source reviews reported no actionable findings.

The earlier v1 gate remains failed: its HTTP fixture called harness.stop, which
closed/nilled its httptest server before session bootstrap. The panic happened in
the test helper before the new handler was reached. The correction stops only
workers, following the existing payment API fixture. The failed log, source freeze
and diagnosis remain separate. These are mock IAM/Loom/Telegram component tests,
not actual browser/native UI, real Telegram, unattended-model or production proof.
Temporary container pairs and labeled volumes are removed after the gates; main
service fingerprints remain unchanged.

The [owner form](shuttle-miniapp-order-proof-ui.md),
[reviewer form](shuttle-miniapp-order-review-form.md) and reactive
[owner](shuttle-miniapp-order-proof-context.md)/
[reviewer](shuttle-miniapp-order-review-context.md) context now have separate
source verification. Next: authorized notice delivery and real
native/agent/mixed/restart acceptance on the owned stand. Publication and
production remain held.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
