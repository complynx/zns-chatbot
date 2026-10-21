# Reviewed release candidate, 2026-10-21

Source `3be6e40bdc6131369fca278033af77d3719c67b6` passes
[CI46](https://github.com/complynx/zns-chatbot/actions/runs/39664578385):
870 PostgreSQL/race integration roots in nine batches, 369 UI tests, 23 stand
guard tests, 29 Telegram simulator tests, seven integration-runner tests,
generated SQL, lint with zero issues, and Linux/container builds. The tested PR
merge is `4822433a17120e05ad036bad652ea12139db935b`; its parents include this source.

The separately built Linux candidate is
`sha256:e23375962bda17b0ffc7d5b34e220e95bf03cccb0169dfc10739955cc304ef2c`.
Both binaries were read back from an owned stopped probe and matched their build
hashes. The probe was removed. Its runtime base, Go version, dependencies and
container settings match the installed meal image; only the two application
binaries and source/ownership labels were layered onto it. No schema migration
is required.

## Bounded functional acceptance

One disposable `reviewed-release-r1` stand used actual Zitadel, Loom and the
candidate, with a local Telegram simulator and synthetic identities. It did not
use a real Telegram token or the primary stand's database/IAM/network.

The check completed 19 native tasks and one agent task:

- With Loom stopped, `/start`, `/menu`, `/profile`, `/registrations`, `/register`,
  `/orders`, `/massage`, `/avatar`, `/pairs`, `/payments` and `/knowledge` returned
  native responses. These are entry-point checks, not full business workflows
  for every module.
- Native event selection and a leader callback created one waiting-list entry.
  Replaying the identical Telegram callback update created no second task or
  registration. An administrator assigned one pass through the authorized API;
  the owner read its deadlines in `Asia/Yekaterinburg` through `/registrations`.
- Native `/name` created an expectation without a Loom run. The next ordinary
  message started one actual Loom run. Its model request contained that same
  expectation ID. Through the controlled provider, Loom read the profile and
  called `complete_name` with the matching ID/version. The name was saved and
  the expectation cleared. Subsequent native profile/pass reads showed the same
  data, with one registration and one pass.
- SIGTERM stopped the candidate in 0.969 seconds with exit 0 and no OOM. The same
  container was started once. Profile, registration, pass and their two operation
  tables matched the pre-stop values. Native reads still worked afterward;
  no second Loom run, registration or pass appeared.

The provider used three controlled model responses and synthetic usage counters.
This does not prove unattended model quality or real Telegram/WebView behavior.
The graceful check began with completed work; recovery during active HTTP/work
remains covered only by its separately scoped reports and tests.

A fresh independent review found no actionable issue for this stated scope.
The nine services, their volume and networks were removed after all three
PostgreSQL databases were dumped and fully decoded. Logs, the Telegram journal,
checksums and checkpoints are retained under the ignored
`.cache/shuttle-work/reviewed-release-20261021-v1/` directory. The primary's domain,
Loom state, configuration and container identities passed preservation checks.

## Original failures retained

The first image-packaging attempt used a bare local image ID in `FROM`, which
BuildKit interpreted as a registry reference. No candidate was created. The
corrected attempt used the existing local tag after verifying its immutable ID.

Three QA harness observations were corrected: an absent run is an empty string,
message snapshots arrive newest first, and readiness is plain text rather than
JSON. The completed command prefix and already restarted container were inspected
and retained. Commands, event creation and restart were not blindly replayed.
These records are harness failures, not evidence of product defects.

## Installation boundary

The owned primary now runs this candidate from source `3be6e40b`, still
schema69 / mvp-89. Only the bot container was replaced. The original poller ID
was resumed once; all other companion identities, images and start epochs were
preserved. The old bot stopped by SIGTERM in 0.609 seconds with exit 0. A fresh
fenced PostgreSQL dump was fully decoded before replacement; no SQL migration
or restore was performed.

Readiness, manifest, all 18 installed Mini App assets and six anonymous financial
route boundaries passed. Business tables, sequences, queued work, database
identity, configuration and Loom state passed the preservation checks. The
poller's successful cycle was observed before accepting the installation.
The post-reboot River leader change remains an explicit earlier exception;
its original strict failure was not erased or generalized to future restarts.

The installation procedure passed fresh independent reviews. One correction
removed inherited pre-reboot process fingerprints; another limits worktree-source
validation to installation stages. Read-only observation of the accepted image
still checks immutable installation/configuration records, exact runtime and
data, while allowing subsequent Go source development. Prior reviewed procedure
and checkpoint versions remain retained. The primary installation is separate
from the bounded secondary workflow acceptance above.

Joint CA/ZitadelTG/real-Telegram Mini App checks, exact native-preparation adoption
through Loom, consent/revocation lifecycle, durable asynchronous tracing, real
legacy migration and production cutover remain separate gates. No production
readiness or deployment is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
