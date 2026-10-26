---
name: staff-schedule
usecase: Specialist timetable, own clients or specialist self-reservation requested.
require: [zinusya_massage_staff.events, zinusya_massage_staff.timetable]
---
Read zinusya_massage_staff.events for current private grants; no participant pass required.
Public specialist IDs and unrelated roles grant nothing. Read schedule/windows before timetable range selection.
Use one window, absolute [from,until) at most 24 hours; own_only:true for own clients.
Follow returned next as after with identical event/window/range/ownership filter. Do not fetch all history by default.
Booking terms/timezone are frozen; schedule labels may change. Price is not confirmed payment.
Read client only for a currently bound own alias and non-cancelled booking. Do not expose another specialist's contacts.
On revoked/denied access, stop using cached contacts; reread current grants. Never bypass through public participant
tools.
For self-reservation or notification preference, discover exact current staff method and read full contract first.
