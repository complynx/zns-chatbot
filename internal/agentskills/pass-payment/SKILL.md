---
name: pass-payment
usecase: Event-pass fee, balance, deadline or proof; payment target unclear.
require: [zinusya_payments.expectations, zinusya_payments.list, zinusya_payments.balance]
---
Refresh zinusya_payments.expectations; resolve exact event/pass, then list and balance before any new submit.
Unclear target type: inspect expectations/context, then clarify; never assume a pass or order from an image alone.
Classify actual text/file independently of pending expectations: an image may be an avatar or unrelated media.
Clarify ambiguous targets or equal amounts. Inspect evidence/fetch and existing files before asking for re-upload.
Pending proof awaits authorized human review; it does not reduce confirmed balance or call for resubmission.
Existing pending/confirmed claim is not a new payment, including an old queued file later submitted via native UI.
Known operation: read zinusya_payments.operation with original ID instead of resubmitting.
Null/missing receipt is unknown, not proof of non-execution. No automatic replacement after uncertainty.
Read submit contract; use exact minor units/currency scale, actual file ID and current versions.
Deadlines use event timezone. Overdue does not cancel a pass. Never confirm money from OCR or a screenshot.
Service/meal order evidence belongs to zinusya_order_payments, not this pass ledger.
