---
name: service-order
usecase: Service catalog, price, selection or order change requested.
require: [zinusya_orders.catalog, zinusya_orders.quote]
---
Resolve event with zinusya_context.list for orders; read zinusya_orders.catalog, then quote for complete total.
Catalog prices are base only; sales_open is a time window, not capacity. Missing pricing/capacity policy is unknown.
Read selection/save_selection and submit contracts; use current event/catalog/draft versions and explicit intent.
Native saved selection is an existing draft, not a fresh request; inspect original operation before continuing.
Preserve frozen parameters/approval. Changed parameters require fresh consent; never duplicate native completion.
Submission creates no confirmed money or capacity reservation. Read get/list for current order state.
Read cancel contract before cancelling. Cancellation is not a refund or fulfillment reversal.
Meal menus use zinusya_meals. Payment proof/review use zinusya_order_payments.
