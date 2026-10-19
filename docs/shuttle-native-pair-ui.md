# Native pair invitation form

Subsequent [isolated runtime acceptance](shuttle-native-pair-runtime-qa-2026-10-19.md)
now covers the form and six native/agent paths. The component-stage next step
below is historical; [Primary installation and real Telegram entry acceptance](shuttle-native-pair-primary-qa-2026-10-19.md)
now pass. The later [current-pair membership gate](shuttle-native-pair-membership.md)
records the installed image, completed navigation-history follow-up and actual
Telegram current-pair read. Earlier component receipts below stay historical.

The component gate passed on 2026-10-19 before primary installation. It did not
close runtime acceptance or the full native/agent pair workflow.
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

The list shows invitation state. An accepted invitation is historical consent;
it does not prove the pair is still active or either pass is assigned. Request
receipts are also labeled historical. HTTP200 with an expired result is displayed
as expired. Current pair lifecycle/assignment projection was not part of this acceptance.
A later [membership component](shuttle-native-pair-membership.md) adds current
pairs and participant leave in source; its runtime acceptance is separate.

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

## Verification and limits

All240 UI behavior tests pass, including13 new pair scenarios. They use the
existing Node DOM/API harness, not a real browser. Cases cover explicit role
consent, invitation/revoke/decline, event version change, lost responses before
and after commit, original-key recovery across reload, storage failure, corrupt
owner data, late logout responses, rejected versus uncertain requests, event and
invitation pagination, inert text and event-zone rendering.

Nine PostgreSQL root scenarios pass without skips: the six pair API/session/
privacy/mixed-path roots, native `/pairs` entry and replay in both languages,
existing start/avatar replay, and preservation of an active agent question.
These integration tests use mock IAM, Loom and Telegram. Both affected internal
Go packages pass. Strict lint reports0 issues. The Linux/amd64 binary is built
from a563-file pinned product source closure and remains uninstalled. CI includes
the new pair UI suite.

Independent reviews found and fixed two recovery defects: an unknown partner
POST404 could trap the form, and an ambiguous retry could retain earlier rejection
certainty. Focused cases cover both. The final fresh review has no actionable
findings. The Git index remains unchanged and empty.

The isolated512MiB PostgreSQL dump was validated without restoring it. The test
container stopped with exit0 using a30-second database timeout, without OOM; its
container and labeled volume were removed. The primary's11 service identities,
schema59/mvp68, domain data and133 Loom runs remain unchanged. No primary or Loom
developer service was changed, and no real Telegram message was sent in this stage.
Ignored evidence is `.cache/shuttle-work/native-pairs-ui-v1/`.

Next at this historical component gate: install the candidate on an isolated
owned real-IAM/Loom mock-Telegram
stand and exercise the actual form in all four native/agent paths. Only after
that perform real Telegram acceptance. CA/zitadeltg interactive linking remains
a separate deferred gate. Full registration/pass parity and production readiness
are not established by this component.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
