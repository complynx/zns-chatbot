# Resume after the reboot pause: owned-stand checkpoint

The owned primary stand was restored with the same eleven container IDs, images,
configuration and database cluster identity. PostgreSQL, IAM, Loom and bot readiness
were checked before the original Telegram poller was enabled. The installed bot
remains schema49 / `mvp-56`, image
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`.
No product code, migration, deployment configuration or developer Loom stand changed.

## Pause and restoration

The user explicitly paused development for a reboot. Work and the heartbeat were
paused. The unused empty massage contention project was destroyed, including its
volumes/network. Primary database volumes were retained. Custom PostgreSQL archives
were saved for the application and Loom, with checked headers and tables of contents;
this checkpoint does not claim a restore exercise.

At pause, the bot exited0 after0.922 seconds, Loom after0.860 seconds and PostgreSQL
after0.859 seconds. The test Telegram guard and counted provider exceeded Docker's
five-second stop budget and exited137. All other primary services exited0. This is
not a claim that every helper handled SIGTERM gracefully. Their detailed outcomes
remain in the pause checkpoint, and an inaccurate shorthand in the private resume
note has an explicit correction.

Docker Desktop was not running when work resumed. It was started once. Existing
primary containers were inspected by exact ID/name/image and restart policy; their
configuration and535 frozen input hashes matched the accepted installation. The
original PostgreSQL container started first. Its database OID/cluster identity and
all saved fingerprints matched before workers started. Dependencies and IAM then
started, followed by Loom, bot and finally the original test-bot poller. A fresh
polling cycle was observed. Existing stopped `qa-a`/`qa-b` projects were not started
or modified by this restoration.

## Preserved data and normal scheduler retention

All domain, schema and sequence fingerprints remained exact after workers started.
The Loom run count remained132. Durable tasks and delivery records
remained quiet and unchanged. Queue semantics were unchanged apart from normal
leader/queue bookkeeping.

The old acceptance observer also required every terminal River scheduler row to
remain forever. That assertion failed after startup maintenance. At the first
inspection,20 completed rows older than24 hours had been removed; the remaining26
rows matched their original rows exactly. At final restoration acceptance, another
eligible completed row had expired, for21 removed rows. No new or changed scheduler
row was accepted by the replacement observer.

This matches River0.49 defaults: completed/cancelled jobs are retained24 hours,
discarded jobs seven days. The application leaves those retention settings at their
defaults. These are scheduling records, separate from the durable application task
and business history. The source-reviewed new observer permits only deletion of
original terminal rows beyond those ages. It still requires exact remaining rows,
unchanged domain/schema/sequence data, original queue semantics, quiet durable
work, unchanged Loom run count, current configuration and exact new runtime epochs.
It does not classify removal of unfinished jobs as maintenance.

## Retained failures and limits

The initial IAM readiness observer incorrectly expected a Docker healthcheck that
the preserved container does not define, and timed out. No service was restarted
because of that timeout. Host discovery through `host.docker.internal` was refused
from Windows; localhost without the configured Host header returned404. Trusted
CA verification against localhost18443 with Host `host.docker.internal:18443`
returned200 and the expected issuer. That existing supported endpoint established
IAM readiness without changing TLS verification or IAM configuration.

The strict historical scheduler-fingerprint assertion also failed as described
above. Both observations were retained; the old accepted evidence was not rewritten.
An independent review of the new read-only observer found no actionable findings
and checked the default retention source and actual row lineage. This checkpoint
proves restoration of the stopped owned stand, not SIGKILL recovery, production
restart acceptance or a complete unattended agent dialogue. No new test message
was sent to real Telegram during restoration.

Private evidence is retained under
`.cache/shuttle-work/reboot-pause-20261018T142108Z` and
`.cache/shuttle-work/reboot-resume-20261018-v1`, with the new observer at
`.cache/shuttle-work/reboot_primary_observer_v1.py`. Future QA uses the new runtime
epoch and explicit retention rules; historical continuous-epoch proofs stay intact.
The empty massage scenario was recreated with fresh ownership and remains a
separate acceptance step.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
