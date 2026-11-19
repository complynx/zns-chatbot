---
name: massage-booking
usecase: Participant massage availability, booking or cancellation requested.
require: [zinusya_massage.schedule, zinusya_massage.availability]
---
For availability or a new booking, resolve the event with zinusya_context.list and read zinusya_massage.schedule.
For free slots, read zinusya_massage.availability. candidate_starts only shows starts allowed by the schedule.
Use the event timezone and current versions. Availability does not hold a slot.
For an existing booking or cancellation, start with get/list. Read the contract for the selected action.
Confirm intended specialist, service, window, duration and price.
Price is not confirmed payment. Verify actual booking result, not merely a proposal or available slot.
For uncertain execution, read operation with the original ID. not_found is unknown. Do not book a replacement.
Atomic reschedule is not implemented. Never cancel an old booking assuming replacement will succeed.
Private staff reads are separate; a public specialist ID or participant pass does not grant staff access.
