# Payment review workspace agent API

Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

The agent adapter for the [shared store](shuttle-payment-review-workspaces.md) is
implemented and component-verified. This adds five dynamically discovered methods
and nine HTTP routes. The [local launch rollout](shuttle-miniapp-local-launch-qa-2026-10-16.md)
installed manifest mvp-39/schema39 on the owned runtime. The native HTTP/session
adapter also passes component checks. The [reviewer UI](shuttle-miniapp-payment-review-form.md) now passes source
component checks and is installed with native entry and reactive context on the
current owned candidate. Real-service happy paths pass with a manual provider;
embedded critical paths and unattended model acceptance remain open.

## Contract

All routes use /internal/agent/payments/ and the existing verified payment identity.
Discovery exposes the new methods only to an event payment authority or verified
global administrator; every protected read/write checks the actual event again.
An assigned pass alone grants no review authority. User/organization/admin flags
are not accepted as arguments.

- review_workspace reads this actor's current decision for event_id/evidence_id.
  Its result is {workspace:...}; workspace:null is an absent current draft.
- save_review is an idempotent no-confirmation intent write with a full normalized
  review and expected_revision. It credits nothing. Same-operation replay returns
  its original snapshot, so read current state before continuing.
- cancel_review is an idempotent no-confirmation write for workspace_id/revision.
  It cancels only the pending review intention, not a pass, proof or payment.
- review_commit requires workspace_id/revision and the unchanged full review.
  Its exact_parameters preparation discloses amount, decision, reason, balance
  version, frozen currency/scale, reported amount and event-local deadline.
  The outer proposal differs from the canonical proposal and never extends expiry.
- review_operation is read-only and takes the original transport operation ID as
  arguments.operation_id. Its envelope echoes the new read call's operation ID;
  result.receipt describes the requested historical operation. receipt:null is
  unknown, never permission to replace an uncertain operation.

save_review/status, cancel_review/status and review_commit/status take only the
original transport operation ID in the envelope. review_commit/prepare is the
remaining ninth route. Every write requires idempotency_key=operation_id. Commit
also requires exact_parameters confirmation with the matching outer proposal.
The transport envelope always returns L; the successful commit result contains
canonical_operation_id=C and the original review, including its balance.

Pending commit status returns pending. It resolves an existing canonical receipt
read-only when another approved interface has completed the same decision.
Without a receipt, cancelled/superseded decisions and expiries checked under the
canonical/event locks using the database clock return cancelled with a terminal
reason. Owned Loom's status decoder handles pending, cancelled and not_found as
pending, terminal failure and unknown respectively. Authentication/authority errors
stay 401/403; storage uncertainty stays 503 rather than a terminal business result.

The standalone review tool/routes remain for existing operation recovery. Its
updated discovery text directs new and shared decisions through the workspace
methods; it is not the way to continue a pending native workspace. The shared
canonical domain gate still prevents old review entry from bypassing cancellation.

## Component evidence and limits

The broader Linux/race gate passed the exact expected 51 roots/73 pass events over
integration, payments, API, database and task packages. API package had no matching
roots; new HTTP coverage is in four integration scenarios. They cover exact money
approval/discovery schema, pending/no credit, current reads, parameter rejection,
commit/status replay, missing status uncertainty, store-to-HTTP canonical recovery,
cancellation, database expiry and role revocation. The store-side alias is a test
of the shared seam, not a real native form or Telegram/Loom run.

After that pass, only the new manifest source changed: workspace_id and 128 literals
became equal named constants. Four HTTP integration roots passed again under Linux
race on the final831-input snapshot; other selected packages compiled with no matching
roots. This is a scoped final proof, not another 51-root run. Final API/database/
integration lint passed with zero issues. Initial lint failures remain in the first
API proof directory. Four fresh independent source reviews found no actionable findings.

Both disposable PostgreSQL/Go container pairs and database volumes were removed;
the ten main containers retained IDs/images/start times/restart counts/running state.
Limits were 512MiB PostgreSQL and2GiB Go without extra swap, with no OOM. No commit,
push, PR, runtime activation or production deployment was performed during this
component gate. The later owned schema39 rollout is recorded above.

Retained receipts:
- .cache/shuttle-work/payment-review-api-qa/checkpoint.json: broader51-root gate.
- .cache/shuttle-work/payment-review-api-final-qa/proof.json: final4-root gate,
  manifests, lint and the exact distinction between source snapshots.

The later native store and HTTP adapter provide transactional session checks,
receipts and protected proof download. The reviewer form, native menu and bounded
reactive context are installed. Four happy paths now pass in the real Telegram
iframe with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
