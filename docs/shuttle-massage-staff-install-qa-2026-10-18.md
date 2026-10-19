# Owned specialist-feature installation — 2026-10-18

Status: PASS for forward installation on our local `shuttle-live-test` stand.
Runtime is schema48 / `zinusya` manifest `mvp-54`, using the reviewed image
`sha256:72acda129fa105c9830de1bb8a1c6ca28616aecb8f1823c981e0853d94e7ada3`.
This is not full staff-workflow acceptance, live rollback acceptance or production
readiness. Real-Telegram staff/admin native, agent and mixed workflows remain next.

Subsequent [Real Telegram native staff QA](shuttle-massage-staff-native-real-qa-2026-10-18.md)
now passes administrator grant/revoke/restore without an agent run, no-pass
specialist timetable/own-client/contact reads after removing temporary IAM admin
roles, one actual Loom read/reply, and same-container bot restart. Synthetic
fixture bookings and manual provider responses bound this acceptance. Temporary
admin grants are removed; no task is pending. Agent-only administrative writes,
both form/agent handoff directions and complete live-controller rollback remain
open. No production or unattended-model acceptance is implied.

## Forward installation

The owned poller was stopped, accepted business work drained, and only the bot
stopped with a five-second maximum grace. It exited in 0.657 seconds. PostgreSQL,
Zitadel, Loom, five other Compose companions and the standalone counted provider
retained their container/image/start/restart fingerprints.

A fresh private schema47 custom archive (2,696,222 bytes) passed SHA-256/TOC
checks and actual restoration through one owner-labelled PostgreSQL client
container. All 106 recorded public data/schema/sequence fingerprints matched.
This fresh archive replaces the earlier rehearsal archive as this installation's
recovery point. Its verified database remains fenced against new connections;
it is retained for rollback and uses no application process or helper container.

One owner-labelled migrator ran the pinned image. It exited 0 without OOM.
Goose retained every original history row and added exactly applied version48,
with the expected sequence change. Exactly three specialist-binding tables were
added empty. Existing table data, other sequence state/configuration and the old
tables' columns/indexes/constraints matched before application activation.

The controller recreated only the bot with an image-only Compose override. Its
resolved configuration and runtime settings matched the recorded values. Readiness
returned HTTP 200 and the private manifest reported `zinusya` / `mvp-54`.
Post-start comparison of old public tables found changes only in Goose history,
River leader and River queue metadata. Every old business-table hash matched.

The same poller container resumed and completed a fresh polling cycle. Its first
immediate cycle observation was too early and returned pending; the next command
observed that same live ID successfully. It was not restarted or recreated again.
The journal reached `accepted48`. Both terminal restore/migrator containers and
both temporary credential files were removed after exact identity/ownership
checks. The two reusable mock QA stacks remain stopped. No QA message was sent
through real Telegram during this installation.

## Controller verification and remaining boundary

The ignored controller uses an exclusive OS lock and atomically replaced,
flushed checkpoints. Each advance performs one phase. It records job intent and
observes exact IDs; an observation timeout never authorizes replaying a migrator.
Old-image activation requires recorded schema47 history, expected original or
restored OID, original settings and a pinned old image/resolved Compose model.

Sixteen focused tests pass with synthetic SQL/Docker/HTTP boundaries and a real
cross-process OS-lock test. They cover incompatible/foreign database activation,
Compose drift, OOM recovery with pending work, terminal cleanup resumption,
uncertain and completed jobs, fenced restore reuse and missing terminal receipts.
Real PostgreSQL migration/archive/promotion evidence is recorded separately in
[the recovery gate](shuttle-database-recovery-qa-2026-10-18.md).

Independent source review completed three passes, with separate correctness and
operations reviewers on the first pass. Verified fixes: an OOM-killed candidate
blocked rollback, rollback observations omitted terminal job receipts, and the
terminal rollback phase could not resume cleanup. Final review found no actionable
findings. The forward path above was then executed one phase per command.

The controller also implements retained48 archive restoration and quarantine
rollback. That complete controller path has not been executed on the primary
stand. Separate real-PG component evidence and synthetic boundary tests do not
prove live application/poller rollback. No migration Down was executed on the
primary database. A later rollback must preserve new candidate data and reconcile
any accepted work retained in the quarantined48 database; the47 recovery point
predates that work.

Ignored evidence and implementation:

- `.cache/shuttle-work/massage-staff-install-v1.py`
- `.cache/shuttle-work/test-staff-install-v1.py`
- `.cache/shuttle-work/staff-install-controller-v1-qa/checkpoint.json`
- `.cache/shuttle-live-test/massage-staff-install-v1/checkpoint.json`

Next: real IAM/Loom/Telegram specialist/admin native, agent and mixed acceptance,
including browser forms and a no-pass specialist. Keep complete live-controller
rollback and unattended-model acceptance explicit as unproven gates. No production
or Git publication was performed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
