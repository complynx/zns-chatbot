---
name: massage-booking
usecase: Participant massage availability, booking or cancellation requested.
require: [zinusya_massage.schedule, zinusya_massage.candidate_starts]
---
Resolve event with zinusya_context.list for massage; read zinusya_massage.schedule and candidate_starts.
Schedule configuration is not current availability. Use server candidates, actual timezone and current versions.
Read book/get/list/cancel contracts. Confirm intended specialist, service, window, duration and price.
Price is not confirmed payment. Verify actual booking result, not merely a proposal or available slot.
Known operation: read operation with original ID; no replacement booking after unknown outcome.
Atomic reschedule is not implemented. Never cancel an old booking assuming replacement will succeed.
Private staff reads are separate; a public specialist ID or participant pass does not grant staff access.
