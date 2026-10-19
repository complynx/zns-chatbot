# Administrative massage booking history

Date: 2026-10-17. Source: schema 46 / manifest `mvp-50`.
Owned runtime is now schema 46 / `mvp-50`; the new read API is installed.

## Contract

`zinusya_administration.massage_bookings` reads retained bookings and cancellations
across owners for one deployment event. Its POST endpoint is
`/internal/agent/administration/massage_bookings`. Arguments are `event_id`,
optional `after` UUID, and optional `limit` from 1 to 20 (default 20).

The caller needs current global `zinusya.admin` authorization and a valid local
identity binding. Event authority, an assigned pass or a public specialist ID does
not grant this read. The event must belong to the caller's bot and organization.
An active pass or enabled massage module is not required for historical access.
Ordinary participant reads remain restricted to their own bookings.

Each result contains `event_id`, current event `timezone`, UTC `observed_at`, and
`bookings`, whose entries contain `user_id` and `booking`. Booking terms retain
their historical snapshot, including their timezone. The response does not add
legal names, passport data or Telegram credentials. It does not calculate current
availability or grant specialist write permissions.

Pages use ascending booking UUIDs. Follow `next_after` as `after` until absent.
A page can contain fewer than `limit` entries because its encoded result is
bounded to 60 KiB, leaving room for the 64 KiB response envelope. When shortened,
the cursor identifies the last returned booking. A page uses one database
snapshot; successive pages do not share a frozen export snapshot. Concurrent
new bookings can sort before an already-consumed cursor.

## Verification

Six selected Linux Go 1.27.2/PostgreSQL 17 integration test roots passed with the
race detector, zero failures and zero skips. Three new HTTP tests cover multiple
owners, retained cancellations with the module disabled, global-role denial, independent
bot/organization isolation, and byte-bound pagination without lost or duplicate
records in a stable dataset. Three neighboring agent/native tests cover original
status, mixed handoffs, approvals and configuration/candidate reads.

Independent correctness and skeptical quality reviewers found no actionable
findings. The correctness reviewer used a shell command only to read the supplied
diff despite the review's no-command instruction; neither reviewer ran tests,
queried history or changed files. The first lint run reported five findings.
Reusing schema constants and the
existing page-limit constant, and correcting a test append assignment, resolved
them. Final configured lint passed with zero issues. A fresh source reviewer
(`massage_admin_final_v2`) found no actionable findings after these changes.
The six-root Linux/PostgreSQL race gate then passed again on frozen final source.

Frozen-source checks passed. The ten main Compose service fingerprints remained
unchanged. Both disposable test containers and their private volume were removed;
no test process was OOM-killed. Limits were 2 GiB for Go and 512 MiB for PostgreSQL,
with swap disabled. Evidence is retained locally in ignored
`.cache/shuttle-work/massage-administration-v1-qa/` and
`massage-administration-v2-qa/`. The v2 setup initially lacked its evidence
directory and explicit source-freeze file. Those preparation errors were retained;
no Go test runner had started. After setup was completed, the same prepared
PostgreSQL served the successful final run and was removed after acceptance.

The initial compile attempt failed because a test supplied an extra argument to
`CancelOwnMassage`. That call was corrected. A subsequent native integration run
skipped without a database DSN and was not counted as acceptance. The later Linux
PostgreSQL run above executed all six selected roots.

## Owned installation and real-service acceptance

The full release Dockerfile built image
`sha256:daa73e776e3c17d5da724e3b8455fa9e9ad2b41c198bdca243833ff205ea195e`
from 827 frozen release inputs. The dedicated builder had a 2 GiB/no-swap limit,
two CPUs and build parallelism one; its container and cache were removed after
success. This was a local build, not publication.

Independent rollout reviews found missing live-Compose drift checks, weak ingress
acceptance, missing recovery entry points before recreation and overly broad
River preservation exclusions. All four findings were fixed before runtime
mutation; the final driver review found no actionable findings. Preflight proved
that only the bot image would change. A private PostgreSQL dump was created and
its TOC verified before recreation. The old bot stopped cleanly in 0.672 seconds,
exit zero, without OOM or forced kill.

The candidate retained schema46, keys, environment, mounts, security settings,
ports and networks. Data hashes included durable River tables; only leader state
and queue `updated_at` were excluded. Eight companion fingerprints remained
unchanged. The same poller container resumed with an unchanged restart count;
its fresh successful polling cycle and stable running state were observed before
installation acceptance.

Real Zitadel tokens for an existing local fixture administrator read all five
retained event bookings through one-record pages. The ordinary owner received
403, a missing event received 404, and booking IDs, owners and states remained
unchanged. Real Loom run `26a423e8-9e7c-4244-80d7-821cb31fd794` discovered the
administrative method, called it through `script`, received the expected booking
and owner IDs, and completed. The manual provider supplied one script request and
one final response. It was not an unattended model test. No Telegram messages
were sent during this administrative check.

Ignored evidence directories: `massage-administration-images-v1-qa/` under
`.cache/shuttle-work/`, and `massage-administration-main-v1/` plus
`massage-administration-real-v1/` under `.cache/shuttle-live-test/`.

## Remaining acceptance

Specialist identity bindings, staff writes, rescheduling and reminders remain
separate work. This checkpoint does not establish unattended model behavior or
production readiness.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
