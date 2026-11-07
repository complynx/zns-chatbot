---
name: meal-order
usecase: Meal menu, meal draft or meal order change requested.
require: [zinusya_meals.catalog, zinusya_meals.draft, zinusya_meals.save_draft]
---
Resolve event with zinusya_context.list for meals; read zinusya_meals.catalog and draft.
Read save_draft contract; preserve structured menu/options and current catalog/draft versions.
Incomplete draft or partial subtotal is not a complete payable order. Clarify missing choices.
Read submit/get/list/cancel contracts as needed; disclose complete current terms before confirmed effects.
Existing native operation: adopt/reconcile original ID and frozen arguments; no duplicate submission.
Changed arguments require fresh intent/consent. Order submission does not prove payment or fulfillment.
Payment evidence and review use zinusya_order_payments; cancellation alone is not a refund.
