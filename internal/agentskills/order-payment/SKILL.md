---
name: order-payment
usecase: Service or meal order payment or proof; excludes event-pass fees.
require: [zinusya_order_payments.list, zinusya_order_payments.submit]
---
Resolve persisted order kind and exact order/event. Use zinusya_order_payments.list/evidence before submit.
Read the selected order module's get for current order/version. Never substitute pass payment balance or IDs.
Inspect submit contract and actual file; clarify ambiguous target. Existing evidence is not a new payment.
Pending/reserved capacity is not confirmed payment, delivery, fulfillment or a refund.
Known operation: zinusya_order_payments.operation with original ID, not resubmission.
Current order/evidence and original operation receipt differ; report each accurately.
Missing/null receipt is unknown; do not automatically create replacement effects.
