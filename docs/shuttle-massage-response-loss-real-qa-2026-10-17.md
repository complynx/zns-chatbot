# Massage: real Mini App lost response and restart QA, 2026-10-17

Owned runtime acceptance passes for a controlled lost successful booking response,
a clean bot restart and recovery through the original operation in the actual
Telegram Mini App. This is not a production readiness or hard-kill claim.

## Environment and preservation

The authorized account used only its existing private @complynx_test_bot chat.
The real embedded WebView authenticated using Telegram initData and the owned
real IAM setup. There was no locally signed Telegram login fixture or model
provider invocation in this native flow.

The bot retained image
`sha256:904344b47e5852efcacd1020911fcad25ec57969d82a3f6c9a3d9f3755f6adca`,
schema46 and manifest mvp-49. Only the owned Mini App origin/launch temporarily
changed from18382 to a loopback proxy on18383, reached through the existing
server-to-localhost launch redirect. Keys, mounts, ports, user, resource/security
settings and other environment were preserved. A private backup archive was
checked before cutover. Compared business tables were preserved across startup;
River scheduling metadata was explicitly excluded from business-table hashing.
Eight companion containers retained their identities, images and starts. The same
Telegram ingress poller was stopped/drained and resumed for each runtime change.
No production or Loom developer checkout/stand was changed.

## Actual fault and unknown-result state

A fresh native /massage command opened the new18383 embedded app. It created one
normal durable command task and no Loom run. Before form writes, counts were176
Shuttle tasks and118 Loom runs, compared with175/118 before the command. Those
176/118 counts remained unchanged through the form lifecycle and recovery.

In QA massage schedule - SYNTHETIC, Europe/Berlin, the owner selected Fixture Alex,
Synthetic short massage,25October2026 01:00 GMT+02. The preview showed45.00BYN,
treatment until01:15 and occupancy until01:20, with an explicit confirmation.
The existing03:00 GMT+01 booking stayed booked.

The user confirmed once. The proxy forwarded the original proposal/operation and
read a valid upstream HTTP200 containing the committed booked/version1 record.
It then returned a controlled HTTP503 instead of that successful body. This models
a successful write whose result did not reach the caller; it is not physical
packet loss, a power failure or a kill during the transaction.

The actual form displayed an unknown result. Event and specialist controls were
disabled; confirm/discard disappeared. Check earlier request and Retry the same
request remained available. No second submit was sent. Database read-back proved
one new booking and its exact miniapp-source operation/proposal receipt. All four
pre-existing booking rows for the fixture owner/event remained identical.

## Restart and exact original-operation recovery

The same bot container stopped in0.672seconds, exit0, without OOM, and restarted
with its image/configuration/key unchanged. Compared business hashes were identical
across restart; PostgreSQL, Loom, IAM and the other companions stayed up. The
still-open form retained its unknown request.

After the bot was ready, the test armed a unique post-restart recovery phase.
The user clicked only Check earlier request. The proxy recorded the actual GET
for the original operation, its successful HTTP200 and the returned booking ID
under that phase. A pre-restart or unrelated GET cannot supply this proof.
The form displayed Booked, rendered the new reservation in current history,
re-enabled event selection and removed the recovery/retry controls.

The complete new booking row and immutable operation receipt matched their
post-fault snapshots; all four older booking rows for the fixture owner/event remained unchanged. There was one
new booking, one receipt for its operation, and no new form tasks or Loom runs.

Stable fixture identifiers:

- Event:74244b1f-dc3c-452f-8e7d-6e6a083b6323.
- Operation:44ca7490-3654-47fc-a3d1-dc64c1498902.
- Proposal:fc0bed95-5b33-4f3b-a1d5-25de498ee216.
- Booking:3b81f4ed-1f2f-43ea-b072-b3455327be2b.

The WebView was closed, the original18382 origin/launch restored, and the exact
proxy container removed. Business data, image/key/settings and eight companions
were checked again; the same poller resumed. The10 normal Compose services remain
running. The proxy consumed about13MiB under its128MiB/no-swap/1CPU limit; it did
not OOM. The synthetic booking and evidence remain for diagnosis.

## Helper proof and review loop

Final mocked proxy checks passed ordinary forwarding, internal-route rejection,
one successful-body suppression, same-key retry without rearming, one mock effect,
no proof for unrelated GET, no proof before the restart phase, and capture of the
exact phase-bound recovery GET/booking without altering the original fault record.
The mock check establishes helper behavior, not product acceptance by itself.

Four independent review iterations, including separate correctness and skeptical
quality reviews, required full comparison of the new booking row, actual original
operation HTTP recovery proof and a post-restart phase binding. Those gaps were
fixed before stand mutation. The final fresh reviewer found no actionable findings.
Source inspection also corrected the baseline to count the native command task
separately from the no-task/no-Loom form lifecycle.

An earlier mock helper run read recovery.json before the request thread had
finished writing it. That failed attempt remains preserved; a bounded wait fixed
the observation race. Read-only preflight revisions and their earlier guards are
also retained. The Git index stayed empty; no commit, push, PR or issue was posted.
Fault metadata contains only fixture IDs, status and hashes, not authorization,
initData or raw successful response bodies.

## Remaining acceptance

The later [real Mini App precommit cancellation SIGKILL gate](shuttle-massage-hardkill-real-qa-2026-10-17.md)
accepts full-bot cancellation rollback and exact-request retry through the actual
form. Other full-bot boundaries and combined Loom/IAM outage paths remain open.
The separate accepted
[subprocess transaction-boundary gate](shuttle-massage-process-crash-qa-2026-10-17.md)
does not replace those runtime scenarios. Specialist lifecycle, notifications,
remaining parity and unattended model behavior stay open on the roadmap.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
