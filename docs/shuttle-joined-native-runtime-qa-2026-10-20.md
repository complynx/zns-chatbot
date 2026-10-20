# Joined native registration, pair and payment QA — 2026-10-20

One complete two-participant native journey now passes on an isolated schema64 /
`zinusya/mvp-74` stand. Registration, pair consent, two-pass assignment, deposits,
human reviews, top-ups and final account reads all run with Loom stopped. This
closes the native joined gate in the [workflow plan](shuttle-registration-pass-parity-plan.md);
the separate [agent joined gate](shuttle-joined-agent-runtime-qa-2026-10-20.md)
now also passes. Both [mixed joined happy paths](shuttle-joined-mixed-runtime-qa-2026-10-20.md) subsequently pass on candidate mvp75; their installation and presentation boundaries are separate.

## Fresh setup and actual native actions

The owned stand starts with four synthetic identities in real local Zitadel, one
configured event and two event-authority records: administrator and ambassador.
Fixture SQL writes only event rules, deposit policy and authorities. Provision
readback verifies zero profiles, legal details, registrations, pairs, passes and
payment evidence; Loom has zero runs. No direct business API setup creates any
participant workflow outcome.

Both participants use `/register`, the actual event/role callbacks and the Edge
form. Each form saves an independent display name, legal name and passport
presence. Saving fields still creates no registration. A fresh explicit role
choice then creates one leader and one follower waiting-list registration.
Replaying the leader's known role callback preserves the same registration.

The leader's actual pair form sends one invitation to the follower. The follower
first attempts acceptance without the role-consent checkbox: no write occurs.
After explicit role consent, acceptance creates one waitlisted pair.

The event administrator prepares assignment in the actual form. A separate
readback proves zero passes and both registrations/pair still waitlisted.
A fresh browser session selects that exact original proposal, displays both
identities and frozen terms, and explicitly confirms. The upstream successful
response is deliberately dropped. Another fresh context recovers the original
completed receipt through read-only recent-operation lookup; confirmation has
exactly one POST. Two passes, for the original two registrations, result.

Each participant sends a synthetic PDF attachment through mock Telegram, selects
that accepted file in the actual payment form, and explicitly submits 3000 minor
units as a deposit against a 10000-minor BYN pass. Evidence is pending. The separate
event ambassador loads protected source bytes, prepares a decision without
crediting money, then explicitly confirms the deposit. This repeats for both
participants. A graceful bot stop/start follows, preserving the observed domain
snapshot. Each participant then submits 7000 as top-up and the ambassador confirms
it through the same real form workflow.

Both participant account forms and `/registrations` show 100.00 BYN confirmed,
0.00 BYN remaining and the event's `Europe/Minsk` zone. There are two assigned
registrations, one assigned pair, two passes, four accepted files, four evidence
records and four confirmed ambassador reviews, totaling 20000 synthetic BYN minor
units. Each full original submission receipt remains identical and pending after
review; current confirmed evidence and balances are separate reads.

## Browser and agent boundaries

Sixteen browser invocations pass: two required-field forms, invitation and
acceptance, assignment preparation and confirmation, four participant submissions,
four ambassador reviews and two final account reads. Contexts close after each
invocation. Assignment and participant entry drivers sign out; the reused payment
proof drivers close their contexts without claiming logout/revocation. Protected
file response and UI-blob bytes are nonempty and equal. These tiny synthetic PDF
bytes test protected transfer, not PDF rendering or document comprehension.

Three previously accepted browser drivers are copied with recorded source/copy
hashes and explicit changes. Assignment adds a native-only resume path for the
exact previously prepared proposal. The other copies change custody constants
and synthetic actor labels. Fresh normal/adversarial final reviews are clean;
earlier review strengthened the pre-consent database readback. Additional source
checks aligned owner assertions with the actual account/profile response schemas.

Actual headless Edge uses signed mock Telegram initData, a fixture SDK and a
context-local TLS exception. This is not a real Telegram WebView run. Selected
assignment, final account and final ambassador confirmation screenshots were
visually inspected. The product source remains byte-matched to the installed
image build; this gate changes no Go product code or schema and adds no remote CI
or full-repository/race result.

During every financial/native action Loom is stopped, with zero runs. Only after
both passes are fully paid does the stand restart Loom. The four original queued
file IDs all reach actual model inputs, in two completed real-Loom runs. A
controlled provider returns acknowledgements with no tool calls. All bot tasks
and the Telegram outbox then complete, and the observed business snapshot remains
unchanged. This proves deferred intake and orchestration without duplicate
financial action; it does not prove unattended model classification or planning.

## Shutdown observations and cleanup

The restart's `docker stop --time 5` command takes 4.125 seconds, exit0, no OOM.
Its application log reports a completed drain in 10.540193 ms against a 1000 ms
budget, with HTTP and workers completed. Domain readback matches before stop,
after stop and after start. Application drain excludes deferred process cleanup;
the CLI wall measurement includes caller/daemon overhead. Historical daemon events
for the original stop were unavailable. This sample does not establish exact
SIGTERM-to-process-exit latency or full exit within one second, and does not
establish the cause of the difference. The hard five-second observed command
boundary passed; the broader process-timing gate remains open. The later cleanup
drain reports 3.367824 ms, also completed.

All eight non-PostgreSQL services stop before the three final custom database
archives are made. Each archive is fully decoded with
`pg_restore --file=/dev/null`; this is not a fresh restore test. PostgreSQL stops
cleanly. All nine owned services, their volumes and network are removed, with no
OOM. Primary schema64/mvp74, its eleven services and 134 original Loom runs remain
preserved. Git index is empty; no commit or push is made for this QA stage.

Local evidence: `.cache/shuttle-work/joined-native-runtime-v1-qa/` contains
`provision-acceptance.json`, `runtime-acceptance.json`, `checkpoint.json`,
`shutdown-observation.json`, `review-final.json`, `cleanup-proof.json`, browser
results/screenshots and private logs/dumps. Frozen driver review is
`zinusya-joined-native-qa-v4.diff` in the local temp folder. Earlier component and
runtime failures remain in their own stage records; this joined business flow
passed without repeating an uncertain effect.

## Remaining work

The separate agent-only joined journey now passes. Continue with whole mixed
continuation paths, with exact
consent and shared operation recovery. Live deadline/cancellation variants,
concurrent role/authority changes, real Telegram agent mutation, complete process
fault coverage and production cutover are not accepted by this happy-path gate.
Orders/meals, massage and joint zitadeltg/CA work retain their roadmap priorities.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
