# Massage native owner form source checkpoint

Date: 2026-10-17. Source schema 46 / manifest `mvp-47`.
The owned runtime remains schema 45 / `mvp-44`; these forms are not deployed there.
This extends the [backend checkpoint](shuttle-massage-native-http-qa-2026-10-17.md).

## Implemented source behavior

The existing authenticated Mini App loads a massage controller using its current
request capability and bearer. It clears rendered choices/history on logout and
ignores late replies after an owner/session change. The new script is in the
explicit public asset allowlist. No new frontend library or dependency was added.

The form selects an event, party window, specialist and public service, then reads
live starts. Long windows use absolute ranges of at most 24 hours; candidate,
history and event lists follow their cursors. Event times use the configured IANA
zone and explicit offsets so repeated DST hours remain distinguishable. Names and
other labels are rendered as text. English and Russian labels are provided.

Booking and cancellation have separate frozen previews and explicit confirmation.
The preview shows event, party, specialist, service, exact price, start, treatment
end, full occupancy end, timezone and expiry. It does not claim payment, a refund,
or a slot hold. Current own history/cancellation remains usable when new schedule
access is denied. Browsing event pages retains the selected event beside its
active slots; recovered off-page requests retain a visible event identity.

Before sending, owner-keyed session storage retains the original operation and
exact body. Already previewed records also retain bounded event name/timezone
metadata for display. They contain no bearer or Telegram initData. Restoration
never automatically applies a preview. An uncertain apply is recovered by its
original receipt or explicit identical retry. Receipt lookup returning 404 alone
stays unknown and blocks another action. For an authoritative apply rejection
(400/404/409), the form first reconciles that same operation receipt; only a
missing receipt after that rejection permits discarding the rejected request and
choosing again. Expired previews explain that the user must discard/reselect.
Malformed or unwritable recovery storage blocks new writes.

## Verification and review loop

All 119 Mini App JavaScript behavioral tests pass on the local Node runtime,
including 16 new massage cases. There are zero failures/skips, and input hashes
were unchanged during the recorded final run. The existing Go public-asset route
test passes with the new script, and configured API lint reports zero issues.
The preceding Linux/PostgreSQL race backend coverage remains documented in its
own checkpoint; these JavaScript tests use mock HTTP and DOM, not real IAM/Loom.

Massage cases cover booking/cancellation preview and separate confirmation;
reload without automatic apply; lost accepted response; unknown original receipt
and exact retry; storage failure/corruption; logout/owner switch with late reply;
module/pass loss and retained cancellation; en/ru labels; DST offsets and later
absolute ranges; definitive rejection reconciliation with or without a receipt;
idle preview expiry; actual app.js bearer/logout wiring; event paging and both
prepared and unresolved-apply recovery outside the first event page.

Five review passes used eight fresh independent reviewers. Verified findings were
fixed: permanent blocking after definitive conflict/access rejection, missing
feedback after idle expiry, loss of selected event during paging, and hidden event
identity during off-page unknown-apply recovery. The final fresh review found no
actionable findings. Focused tests accompany the fixes. Early test-fixture failures
were preserved: cross-realm object comparison instead of JSON transport, missing
DOM boolean coercion and a missing select options collection. Assertions were not
weakened to pass them.

Recorded evidence is in ignored `.cache/shuttle-work/massage-native-ui-v3-qa/`
through `v5-qa/` plus the earlier fixture-failure notes. The final TAP run and frozen
input hashes are under `v5-qa/`. All four preceding disposable backend QA attempts
have had their exact containers and volumes removed, including failed runs; none
was OOM-killed. The ten main Compose services retain their IDs/images/start times
and restart counts, and the counted-provider fixture remains in use unchanged.
No new UI test container was needed.

## Current limits and next work

This is owner-form source component acceptance. The later [native entry
checkpoint](shuttle-massage-command-qa-2026-10-17.md) records `/massage` menu/command
source acceptance. Reactive pending-proposal/action context remains next. The agent must be
able to discover an earlier native choice; current backend handoff tests transfer
known proposal IDs. Notifications, specialist timetable/exceptions, rescheduling
and full Python parity remain open. Real native/agent/mixed acceptance, including
unattended model behavior, is not established by these mock DOM tests.

Daniel explicitly reauthorized browser access on 2026-10-17. The existing Telegram
Web test chat opened the embedded current-runtime app, which reached Signed in
and rendered profile, registration, orders and review forms. No business action
was submitted in that check. This verifies restored access and existing startup,
not the new massage source. A screenshot was displayed by the browser tool, but
no screenshot file was saved because its filesystem write was denied. Do not cite
a nonexistent screenshot artifact. Production and publication remain unchanged.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
