# Native pair invitation form

The later [current-pair membership gate](shuttle-native-pair-membership.md)
records the installed image, completed navigation-history follow-up and actual
Telegram current-pair read.
Earlier component receipts below stay historical.

The previous [API foundation gate](shuttle-native-pair-api.md) remains historical.

## User path

Use `/pairs` or its menu entry to open the Mini App at `#pairs`. Opening the form
does not create an invitation, pair, registration or consent. The command uses the
normal durable task/outbox and works without Loom. Without a configured Mini App,
it returns a localized unavailable message. `/start partner_...` keeps its existing
agent path.

Share your own partner code, select an event, and enter the other person's code.
The form loads your current waiting-list role, version and event-local deadline.
Sending rechecks that context. If it changed, review it and press Send again.
An invitation pauses the inviter's single-person assignment eligibility until it
expires or closes. It does not reserve a pass or assign the other person.

Received and sent invitations have separate paginated views. Acceptance requires
an explicit checkbox for the displayed complementary role and then Accept.
Decline and Revoke are separate actions. Event deadlines show the event's IANA
zone and UTC offset, including DST. The form supports English and Russian.
No partner financial or passport fields are added.

The list shows invitation state.
An accepted invitation is historical consent;
it does not prove the pair is still active or either pass is assigned.
Request
receipts are also labeled historical.
HTTP200 with an expired result is displayed
as expired.
Current pair lifecycle/assignment projection was not part of this acceptance.

## Recovery

Before a write, sessionStorage receives the owner, operation kind, original key
and exact arguments. New writes are refused if this cannot be stored. A lost
response keeps that operation; reauthentication as the same owner can check its
receipt or explicitly retry the same body. Another owner uses a separate storage
key. No token is stored by this controller.

An unknown outcome blocks replacement actions. Receipt GET404 alone does not
prove failure. A definitive POST400/403/404/409 permits checking and closing the
rejected request; every retry resets that certainty before sending. If the retry
is ambiguous, closing remains blocked. A failed storage deletion keeps recovery
active. Logout invalidates outstanding callbacks and clears rendered private data.
Browser session storage is not cross-device durable storage; server receipts and
invitation history remain the authoritative recovery records.
