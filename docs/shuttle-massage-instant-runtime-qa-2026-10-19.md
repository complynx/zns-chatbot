# Instant self reservation: real Loom and graceful bot restart

Date: 2026-10-19. Scoped agent-only behavior is verified on a separate owned
mock-Telegram project, including recovery checks after a normal bot restart.
Two QA observer failures and their independent read-back reconciliation are
retained below. Both actual Loom/native handoffs also pass the separate gate
described below. Real Telegram WebView acceptance remains open. The primary remains schema49 / `mvp-56`; this candidate is uninstalled.

## Candidate and isolation

The release Dockerfile produced the Linux/amd64 candidate
`sha256:511da947c8fd3b8f2d2c55768b9633667c025fc4e7fca6e39728a2578f4971d3`.
It contains migration50 and manifest `mvp-57`. The dedicated builder used two CPUs,
2GiB without extra swap and one build worker. Source hashes matched before/after
build; its container and cache were removed after completion.

`instant-loom-r2` used that exact candidate, real isolated Zitadel and the existing
frozen Loom image. Telegram and the counted provider were fixtures. Provider tool
choices and final text were supplied manually. This exercises real IAM/token
exchange, Loom execution and bot integration; unattended model interpretation and
real Telegram transport are separate acceptance boundaries. No developer Loom
checkout, developer stand or production service was changed.

A fixture administrator created one current event in `Europe/Minsk`, one service
and a private `alex` binding for a separate fixture specialist. That specialist
had no pass and no administrator grant. The event, schedule and grant were stored
through the existing HTTP APIs. The runtime reported schema50 and `mvp-57`.

## Verified behavior

The specialist's mock Telegram message requested a self reservation. Real Loom
called `zinusya_massage_staff.instant`; the service prepared one canonical
proposal. Waiting for explicit consent created no booking or capacity hold.
The delivered approval was compared with the original snapshot before consent:
self-reservation purpose, event, party, specialist, service, price, exact start,
treatment end, occupancy end and named event timezone all matched. Internal UUIDs
and version/argument fields were absent from that approval.

Only the QA bot received SIGTERM. Its stop command completed in approximately
0.609s, with exit0 and no OOM; it was started in the same container/image. This is
a single measured result for this fixture. The initial native/browser component
and earlier runtime attempt remain separate evidence.

After startup, independent reads verified the original task/run, approval ID/hash,
proposal, expiry and frozen terms. Other QA containers retained their runtime
fingerprints. No booking existed before consent. The user's ordinary free text
continued that same original Loom run through `interaction_resolve`.

Final persistence contains two completed bot tasks (request and steering input),
one Loom run, one original proposal, one booked self reservation and one operation
receipt. The booking snapshot equals the original proposal snapshot. Two owner
reminder records remain; there is no initial specialist booked notice. All three
task outbox rows are delivered. No payment confirmation or refund was created.

## Findings and verification limits

The first candidate displayed a redundant raw argument block after complete
service-generated human terms. A focused behavioral test reproduced that output
before the fix. The renderer now treats `zinusya_massage_staff.instant` like the
existing complete massage quotes. An empty quote retains the literal fallback.
Four focused integration roots pass, covering the new behavior, existing
book/cancel quotes, specialist-access quotes and complete generic parameters.
Configured lint reports zero issues; independent patch review is clean.

The first runtime attempt's readiness probe expected JSON. The endpoint returns
HTTP200 with an empty body, so the probe failed after the actual stop/start.
Original state was read back without repeating restart or creating another
proposal; consent then completed the original run. Its redundant argument block
remained a recorded usability finding for that candidate.

On the corrected candidate, a check immediately after HTTP readiness failed to
observe `awaiting_user`. The intermediate task state was not captured. Readiness
checks accepting/database availability; queue restoration proceeds asynchronously.
Later reads verified the unchanged original waiting state and zero bookings.
The observer now waits up to45s for restoration while requiring the same run and
approval identity. The actual restart was not repeated. The original failed
command, reconciliation and successful consent continuation remain distinct
results; this record does not relabel either failed QA command as a clean run.

The final transcript also shows both owner reminder kinds firing for this late
self reservation, with identical visible text. These are separate reminder jobs,
not repeated booking effects or retries of one Telegram send. The legacy Python
notifier can also catch up both kinds, with separate localized messages. Reminder
presentation and late-booking policy need their own parity/UX follow-up. This gate
retains that observation and makes no new coalescing policy.

## Actual Loom and shipped-form handoffs

A fresh `instant-handoff-r1` project used the same candidate, real Zitadel/Loom,
mock Telegram and manually supplied provider decisions. Installed Edge executed
the shipped HTML/JavaScript in separate fresh contexts. Only the Telegram SDK
was replaced with signed fixture initData; external network requests were blocked.

Agent to form: Loom prepared the original self-reservation proposal. The native
form adopted its exact ID, arguments, snapshot and expiry, then created one booking
on explicit confirmation. A subsequent user message resumed the original Loom
run and returned that same booking ID and original booking timestamp. No second
booking or replacement proposal appeared.

Form to agent: the user explicitly cancelled that first booking in the form,
then prepared a fresh self reservation without confirming it. The native pending
proposal appeared in actual provider input. Loom adopted the original proposal;
the entire preparation row, including expiry, remained unchanged before consent.
The delivered approval matched the frozen human terms. Explicit free-text consent
created one booking with source `agent`. A newly opened native form read that
booking and its exact snapshot from history, with no unfinished choice and no
additional instant-confirmation POST.

Final state: two bookings (one cancelled, one booked), three preparations (two
instant proposals and one cancellation), four operation records, four reminder
records, four completed bot tasks and two Loom runs. All six outbox records were
delivered. Browser results and screenshots passed source and visual checks. This
proves integration with scripted provider choices, not unattended model reasoning.
The first source-review pass found premature checkpoint success; it was corrected
before execution. A fresh second review found no actionable findings. All eight
fixture/setup and scenario phases passed; no uncertain effect was replayed.

The project retained structured ledger/transcript, screenshots, validated app/Loom
dump listings and stdout/stderr logs, then removed its nine containers, volumes
and network. Primary runtime and domain data remained unchanged. Ignored evidence:
`.cache/shuttle-work/specialist-instant-handoff-v1-qa/`.

## Current permission at pending consent

A separate `instant-revoke-r1` project used the same candidate and fixture model.
Actual Loom prepared one self reservation with zero booking effects. The fixture
administrator revoked the private specialist binding and then regranted it at a
new version. Applying the original proposal through the real authenticated API
was rejected both while revoked and after regrant. The complete original proposal
and waiting approval remained unchanged.

The user's explicit free-text consent then continued the same Loom run. Its
original write operation ended `failed`; no booking, operation receipt or reminder
was created. Recorded database time before both API probes and their results had
at least one minute of margin before proposal expiry and the booking grace limit.
The durable Loom failure timestamp also met those bounds. Expiry therefore does
not explain this expected rejection.

One QA observer failed because it expected the API error code in the model's
`interaction_resolve` output. The actual output is only `{"status":"failed"}`.
Read-back verified that exact output and the matching original durable failed
write, run ID, proposal, arguments and valid time. The observer was corrected for
future runs. A separately reviewed reconciliation supplied only the final model
reply; approval and dispatch were not repeated. The failed command and original
observer source remain retained, rather than being reported as a clean rerun.

Final state: one unchanged instant preparation, zero bookings, operation receipts
or reminders, two completed bot tasks, one Loom run and three delivered outbox
records. This checks revoke/regrant around uncommitted consent. Historical receipt
recovery after revocation remains covered by the existing store/HTTP checks; it
was not exercised with real Loom here. Scripted provider decisions and mock
Telegram still bound acceptance. Ignored evidence:
`.cache/shuttle-work/specialist-instant-revoke-v1-qa/`. Both database archives and
stdout/stderr service logs were retained before this project's containers, volumes
and network were removed. Cleanup reverified primary runtime/domain preservation.

## Original receipt after binding loss and competing table use

A fresh `instant-receipt-r1` project used the same candidate, real IAM/Loom,
mock Telegram and manually supplied provider decisions. Installed Edge exercised
the shipped form with signed fixture initData. Source review found two QA gaps
before scenario execution: receipt assertions initially did not compare the
actual model output, and competitor ownership used a field absent from the wire
receipt. Both were corrected; a fresh third source review found no actionable
findings. All eight setup/scenario phases then passed without replay or observer
reconciliation.

First, actual Loom prepared a specialist self reservation. The form adopted and
explicitly confirmed it once. The private specialist binding was then revoked.
A new preparation was denied with HTTP404. The user's explicit message continued
the original run, whose original write succeeded by recovering the committed
result. The actual model output, persisted Loom write result and both bot
operation receipts exactly matched the original native wire receipt. Database
links retained the original booking/preparation IDs; the complete booking row,
including snapshot, source and booking timestamp, remained unchanged. There was
one booking and one preparation, with two receipt aliases rather than two effects.

The same owner explicitly cancelled that booking through the authenticated API
while the specialist binding was still inactive and the owner had no pass. The
cancellation succeeded without a specialist notice. This cancellation was an API
fixture action, not an agent or browser cancellation in this gate.

For table competition, the administrator restored the original private binding,
updated the schedule and bound a different user to a different specialist. Both
required the single shared table. These configuration changes completed before
Loom prepared a new original proposal at the current versions. Preparation held
no capacity. A separate fixture API confirmation booked the other specialist for
the same frozen start. The original proposal row remained unchanged and unconsumed.

Explicit user consent continued the original waiting Loom run. Its write ended
`failed`, with only the competitor booking active. Before and after consent,
database time retained at least one minute of margin before expiry and the
booking grace limit. The two owners and specialist aliases were distinct; the
original owner's earlier booking was cancelled. These controls distinguish table
exhaustion from expiry, own-booking overlap, specialist overlap and old grant or
configuration versions. No replacement proposal or booking was created.

Final state: two bookings (first cancelled, competitor booked), four preparations,
four operation receipts, four reminder records, four completed bot tasks, two
Loom runs and six delivered outbox records. Structured ledger/transcript, the
original model-visible/write results, browser result and screenshot are retained
in `.cache/shuttle-work/specialist-instant-receipt-v1-qa/`. Both databases and
stdout/stderr service logs were archived before the nine containers, volumes and
network were removed. Cleanup verified primary runtime/domain preservation. This
is scripted-provider/mock-Telegram acceptance; real WebView and unattended model
reasoning remain outside scope.

## Evidence and cleanup

Ignored evidence roots:

- `.cache/shuttle-work/specialist-instant-readable-v{1,2}-qa/`: reproduced failure,
  four passing roots, lint and source checkpoint.
- `.cache/shuttle-work/specialist-instant-images-v{1,2}-qa/`: source freezes,
  release builds, image metadata and exact builder cleanup.
- `.cache/shuttle-work/specialist-instant-runtime-v{1,2}-qa/`: fixtures, original
  approval transcripts, restart measurements, observer failures, read-back
  reconciliation and final state.

Both runtime projects were archived and destroyed after their checks. Each
retains separate application/Loom database dumps, validated archive listings and
service logs from stdout/stderr. The final project also retains the transcript and
structured business ledger. Cleanup verified removal of project containers,
volumes and network. Archive validation here checks readability, not restoration.
Primary installed files, container identity and domain data were preserved; its
Loom run count remains132. No commits, publication or primary installation are
part of this gate.

Next: owner-reminder presentation, specialist-before-session parity, then real
Telegram/WebView verification. Joint browser CA/zitadeltg verification remains
reserved for the agreed session with Daniel.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
