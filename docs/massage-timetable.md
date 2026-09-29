# Browser massage timetable

An event specialist or event administrator can open the timetable from the
Telegram `/massage` menu. Ordinary customers keep their booking interface and
do not receive access to other clients' appointments.

The page is `/miniapp/massage?event=EVENT&lang=en` (or `ru`). The historical
`/massage_timetable` page path remains available. Launch through the actual
Telegram WebApp button: the signed Telegram data arrives in the URL fragment.
In the synthetic stand the fake Telegram WebApp button supplies the matching
signed launch data. Do not invent an identity parameter or use a foreign owner's
header to select the viewer.

The page reads `GET /miniapp/api/massage/timetable?event=EVENT` with
`Authorization: tma SIGNED_INIT_DATA`. The gateway validates Telegram launch
data, resolves the current identity and asks the event-authorized Core API for
the calendar. Every refresh rechecks permissions. Expired or invalid launch data
requires reopening the page; unsigned direct navigation must not reveal client
details. Responses are not cached.

The timetable is read-only. Party selection preserves the historical massage
rule: only parties with `is_open=false` are included. `is_open` is a legacy party
category, not a switch that enables massage booking. Parties marked `true` are
excluded even when a specialist has working hours there.
It shows party selection, specialists' working hours
and bookings with client name, start/end, duration and BYN/RUB prices. Displayed
times use Europe/Minsk. "Only mine" filters specialist columns for the acting
user. Zoom changes the timeline scale; horizontal/vertical scrolling remains
available. A current-time line appears when the visible range contains now.

English and Russian labels, keyboard controls, mouse and touch are supported.
The refresh button, minute refresh and return to a visible page reload current
data. A successful same-party refresh should preserve the viewed position.
Changing party selects an appropriate initial position. Pending/failed or denied
refresh must remove old private booking content, rather than leave stale client
details on screen. Cancellation and permission revocation must become visible
on the next refresh.

This page does not implement the separate historical username/Telegram-consent
browser-session flow. The Mini App uses signed Telegram launch authentication.
Implementation and builder checks do not constitute independent acceptance;
follow the current stage state in `PROGRESS.md`.
