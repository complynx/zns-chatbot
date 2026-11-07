---
name: order-payment-review
usecase: Human review of service or meal payment evidence requested or resumed.
require: [zinusya_order_payments.review_workspace, zinusya_order_payments.review_commit]
---
Use zinusya_order_payments.evidence_for_review and review_workspace with current target reviewer authority.
Read save_review/review_commit contracts; this schema differs from pass review, with no credited-amount field.
Save explicit intent with expected_revision; saving does not approve money. Commit frozen full review and
workspace_id/revision only after exact human confirmation. Changed arguments need new revision and approval.
Native/agent completion share canonical operation; transport operation_id is distinct.
Read review_operation with original transport ID after uncertainty. Null/missing receipt is unknown.
Prepare/cancel receipt is not payment; reread current order/evidence after recovered original receipt.
cancel_review only stops pending workspace, not completed money, fulfillment or refunds.
