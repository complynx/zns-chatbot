# Knowledge primary installation, 2026-10-20

Status: accepted on the owned `shuttle-live-test` stand at `mvp-83/schema67`.
This is local installation evidence, not production deployment or full mixed-path
knowledge acceptance.

## Full CI prerequisite

[Run39478991989](https://github.com/complynx/zns-chatbot/actions/runs/39478991989)
at `832c1d7e` finishes successfully:814/814 integration roots with PostgreSQL and
the race detector,344 UI tests, stand guards, Telegram simulator, alerts,
dashboard queries, pinned tools, generated SQL, lint and Linux/container build.
The complete terminal log is retained locally. Earlier failed/cancelled runs are
preserved in the [CI follow-up](shuttle-ci-followup-2026-10-20.md).
The installed image was built and tested separately; the controller verifies its
identity and permits only the recorded renderer-test difference in runtime source.

## Installation and original verification failure

The existing poller and bot stop gracefully: Docker stop command wall times are
0.437s and0.672s respectively, both with exit0. The pre-migration PostgreSQL
archive is2,972,623 bytes and is fully decoded before migration. Its SHA-256 is
`7cee39831c4e1a9d1f47d12fcc37370489aff7c5f3e0be97811964870e0ed43c`.
This is archive decoding, not a restored-database test.

Migrations66/67 execute once and exit0. The first post-migration verifier then
fails its raw legacy-schema hash assertion. The original controller, failed
checkpoint and diagnostic exports are retained. All546 retained DDL sections
match; only dump ordering changes. Reconstructing the legacy dump from the
pre-migration archive reproduces the exact original hash. A reviewed local
controller correction restores that verified section order before comparing the
complete bytes. Changed DDL, extra/missing/duplicate objects and a changed preamble
are rejected in five in-memory negative probes. No migration or business action
is replayed. The original migration container is then removed.

The installed image is
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
Only the bot is recreated. Other companion identities/settings remain preserved;
the original poller is restarted and completes a fresh ingress cycle. All11
services run without an observed OOM. Domain data,135 Loom runs and prior schema
objects remain preserved. The synthetic moderator subscription stays disabled.

## Installed checks and remaining limits

Manifest `zinusya/mvp-83`, schema67 and the exact served knowledge JavaScript
are verified. Real IAM diagnostic reads pass; three unauthenticated API boundaries
return401. Exactly one expected knowledge notice reaches the authorized test
account, with its original receipt and one completed queue job. The accepted
checkpoint and new custody epoch are published; the earlier epoch remains intact.
A separate read-only observer34 verifies that installed state.

The temporary-stand [resolution](shuttle-knowledge-resolution-runtime-qa-2026-10-20.md)
and [compatibility](shuttle-knowledge-resolution-compat-qa-2026-10-20.md) reports
remain separate evidence. Exact native-preparation adoption through Loom,
unattended model quality, joint CA/ZitadelTG acceptance, meal checkout and
production cutover remain open. The separately reviewed order-preview change is
not included in this image. Private installation/diagnostic receipts remain under
`.cache/shuttle-work/knowledge-resolution-primary-install-v1/` and
`.cache/shuttle-work/knowledge-resolution-schema-diagnostic-v1/`.
