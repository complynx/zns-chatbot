# Two customers competing for a massage slot: native browser QA

The current schema49 / `mvp-56` implementation passes an isolated two-customer
native Mini App scenario: both prepared the same slot, their confirmation requests
overlapped, exactly one booked and the other received409. The second owner
discarded the rejected request. After the first owner explicitly cancelled, the
second created a fresh proposal for that same slot and confirmed one new booking.

This used the shipped HTML/JS/native APIs, real PostgreSQL and real Zitadel.
Telegram and its SDK were synthetic, with separate fresh browser contexts. Real
Loom was running as a stand dependency, but this native scenario created zero
Loom runs, agent tasks or Loom write operations. It does not accept unattended
model interpretation, real Telegram WebView behavior, production TLS trust or
production deployment.

## Fixtures and observed lifecycle

The owned `zinusya-mock-massage-contention-r1` project had nine services, private
credentials/CA/databases and loopback ports21540..21545. Four synthetic users
bootstrapped normally: administrator, two customers and a separate specialist.
Only the administrator had global IAM authority. Both customers had assigned
unpaid passes. The specialist had an event-scoped binding, no participant pass
and no global role. Normal administrative APIs published the schedule and binding.

The event used Europe/Berlin, one table, one specialist and a45.00 BYN service:
15 minutes of treatment plus a five-minute occupancy buffer. Each actual form
prepared the same specialist/service/start before either confirmed. PostgreSQL
contained two distinct preparations and no booking; preparation held no slot.

A short owned event-row lock delayed execution. Before releasing it, the observer
recorded both native booking POSTs and two active database lock waiters in its
blocking chain. The overlap was observed0.422 seconds after the start signal,
within the five-second native request budget. One response was200 with a booked
record; the other was409 and had no successful operation receipt.

The losing form reconciled that missing receipt and showed the definitive
rejection. Its user could discard and reload; the form did not remain blocked
as an unknown outcome. The loser could not read the winner's private booking,
and its own history was initially empty. There was no automatic confirmation retry.

After the specialist received the first booking notice, the winner prepared and
separately confirmed cancellation in the form. Only after the cancellation notice
was delivered did the second user choose the now-free original slot, prepare a
new proposal and explicitly confirm. The new proposal ID differed from the rejected
one. The rejected proposal's complete database row stayed unchanged and unconsumed.
The final histories each showed only that owner's record.

## Persistence and messages

Final PostgreSQL checks found two booking rows: the original cancelled at version2
and the new booked row at version1. The first row changed only its state, version
and cancellation time. Both rows' snapshots, selected event/window/service/
specialist, price, currency, source, treatment/occupancy bounds and preparation
IDs matched the corresponding confirmed proposals. Four preparations existed:
three consumed and the original losing proposal still unconsumed. There were
exactly three successful native operation receipts, each matching its original
browser response and consumed proposal; the409 created no successful receipt.
No payment, fee or refund was asserted or recorded.

Three distinct delivered messages reached only the bound specialist: first booked,
then cancelled, then freshly booked. Checks covered distinct message IDs, booking
reference, correct client, action heading, event/service/specialist, price and exact
start/end rendered in Europe/Berlin. This order was controlled by waiting before
the next explicit user action. It is not a guarantee of global worker ordering
for arbitrary concurrent transitions.

## Review, retained observations and cleanup

Independent review before business execution found weaknesses in the QA itself:
cancelling too early could correctly suppress a pending obsolete notice; later
assertions were too weak about message content, date/time and confirmed persisted
terms. These checks were fixed before execution. The final fresh combined review
and independent safety review found no actionable findings. The first actual
business/browser execution passed; there was no replacement booking attempt after
an observation failure. The one-shot browser journal records publication intent
and uses exclusive creation. The owned lock was released and browser closed.

The pre-pause unused stand was destroyed without business fixtures. This run used
fresh ownership after restoration. Evidence is retained under
`.cache/shuttle-work/massage-contention-browser-v2-qa`, including source/runtime
pins, operation journal, screenshots, SQL assertions, mock messages and logs.
The application archive is306071 bytes, SHA-256
`a172372fe9931d2e61df9c4d719386731bae7cda99997ea314ed0913ed424494`.
The Loom archive is79226 bytes, SHA-256
`c098e4c98b3de27acf31f0f5eb8686ee8b3c0a498d7dfd6ee254c080155872f3`.
Custom archive headers and tables of contents were checked; restore was not exercised.

The nine-service project, volumes and network were removed. The restored primary
stand retained its runtime epochs/configuration and exact domain/schema/sequence
fingerprints. Its Loom run count remained132. Normal source-reviewed age-based
terminal River retention remains separate from durable application history; see
[the resume checkpoint](shuttle-reboot-resume-qa-2026-10-18.md).
No Go application code, production, developer Loom stand or real Telegram chat was
changed by this gate. No staging, commit or publication occurred.

This accepts the stated specialist/table contention and explicit recovery scenario,
not a throughput benchmark or every owner/party-limit conflict. Component coverage
for separate owner/specialist/table contention remains documented in
[the booking-store checkpoint](shuttle-massage-booking-store-qa-2026-10-17.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
