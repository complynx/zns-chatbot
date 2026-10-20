# Massage owner/specialist lifecycle, 2026-10-20

The bounded native-booking to agent-cancellation journey passes on candidate
`mvp-78`, schema64. During this gate the owned primary remained schema64/mvp77,
eleven services and134 Loom runs. Candidate78 was not yet installed there.
Its later [local-primary installation/rollback](shuttle-massage-primary-qa-2026-10-20.md)
passes separately.

The disposable nine-service stand uses real local Zitadel, PostgreSQL and Loom,
a controlled provider, synthetic Telegram and shipped forms in headless Edge.
Event, schedule and authority configuration plus an assigned unpaid owner pass
are explicit prerequisites. The specialist has an event binding, no global role
and no pass. No booking, preparation, operation receipt or notice is seeded.

## Whole journey

The owner opens the actual form and previews Alex's Short service at45.00 BYN.
The preview shows the event, party, specialist, service, start, treatment end,
occupancy end, Europe/Berlin zone and confirmation deadline. Separate confirmation
creates one booking and one immutable receipt, without starting a Loom run.
The original preparation is consumed once with its canonical arguments and result.
The slot becomes unavailable. A booked notice reaches the specialist with the
owner contact entity. The due advance reminder reaches the owner once; the final
reminder remains scheduled for later.

A fresh specialist message starts an actual Loom run. Event-bound staff tools
return the same booking, frozen terms and owner contact in the specialist's own
timetable and client details. This run finishes without changing business state.

The owner's next message asks the agent to cancel and wait for consent. The
actual model input contains the native booking's original operation, preparation
and booking references. Read tools recover that booking and immutable receipt.
An exact cancellation preparation freezes the original booking and version.
The delivered question states the original event, party, specialist, service,
price and local times, plus the absence of a payment or refund effect. Before
consent, the booking remains unchanged and no cancellation receipt exists.

Untagged free-text consent resumes the same original run and authorizes the
prepared operation. One agent cancellation receipt consumes that exact
preparation. The booking changes only to cancelled, with its version incremented
and cancellation time recorded. Its original terms, source and booking receipt
remain unchanged. A cancelled notice reaches the specialist.

A fresh browser session shows the cancelled history with the same original terms
and makes the released slot available again. A third actual Loom run reads an
empty active specialist timetable. All three runs and bot tasks/outbox entries
finish. There is one booking, two preparations and two operation receipts.

## Notifications, source and manual checks

Stored payloads and recorded mock-Telegram messages match in recipient, text and
entities. Exactly one advance reminder, one specialist booked notice and one
specialist cancelled notice refer to the original booking. The future owner final
reminder is delivery-blocked after cancellation, without an attempt or receipt.
No specialist soon reminder is delivered. No payment or refund is recorded.

Candidate78 fixes identical long/short owner-reminder texts. English and Russian
now name the advance/final stage while retaining absolute event-local time.
Scheduling, job count, catch-up and financial behavior are unchanged. The new
behavioral test first fails for both languages, then passes after the fix.
Affected task/API package tests, zero-issue lint and a Linux cross-build pass.
The manifest also points to the already available specialist tools. Independent
source and packaging reviews finish with no actionable findings. This is not a
new full Go, PostgreSQL or race acceptance claim.

Two browser invocations verify all17 shipped asset hashes and close their
contexts and browsers. Three final screenshots are inspected manually: exact
preview, booked history/occupied slot and cancelled history/released slot.
The outgoing transcript is also inspected. Actual runtime delivery covers the
English advance label; Russian and final-stage distinctions are unit-tested,
not claimed as delivered by this runtime scenario.

## Preserved failures and cleanup

The first package build fails because BuildKit treats a raw local image ID as a
registry base reference. Its original proof/log remain. A reviewed named local
base tag is verified against the exact accepted image before and after the
successful build, and the packaged binary hash matches the Linux build.

An initial stand name fails length validation before Docker or fixture creation.
The next stand retains two failed browser observations: its test guard first
compares event-local and UTC timestamps as unequal strings before forwarding a
preparation, then rejects the server's canonical UTC arguments after one proposal
is created. No booking or booking receipt is created in that failed stand.
Read-only recovery failures and original results remain preserved. That stand is
archived and removed, rather than relabelled as a successful run.

The fresh successful stand uses reviewed timestamp-instant comparisons for slot
selection. Confirmation still sends the exact returned canonical arguments,
preparation and operation key. Its whole journey passes without an observer
failure. Runtime preflight and the bounded recovery/correction diffs receive
fresh ordinary and adversarial reviews, ending with no actionable findings.

Both real temporary stands stop all nine exact owned services with exit0 and no
OOM. Each stops writers before taking three database archives, fully decodes
those archives and records their hashes. Containers, networks and volumes are
removed. Archive decoding is not fresh restore acceptance. The original primary
is preserved; production, the real Telegram chat and Loom developer systems are
untouched. Ignored current evidence is in
`.cache/shuttle-work/massage-owner-lifecycle-v3-qa/acceptance.json`;
failed evidence remains in the preceding v1/v2 QA roots.

This closes the bounded agent-cancellation and owner/specialist reconciliation
gap. Earlier specialist reservation/access handoffs remain separately accepted.
It does not close every interface interleaving, unattended model comprehension,
real Telegram WebView, all failure/outage paths, atomic rescheduling, charging,
reminder coalescing/backfill policy, production readiness or candidate78 primary
installation/rollback. Meal financial checkout remains a separate open slice.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
