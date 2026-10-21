# Avatar source installation: primary QA, 2026-10-21

## Accepted installation

The owned `shuttle-live-test` primary runs schema 68 and the `zinusya` manifest
`mvp-86`. Installed image:
`sha256:5f912a3dd7476f7270bde921bd372ff217a41e0ee521d243f87c4c1ace7478ac`.
Its source commit is `0599dfefb55913f951981651454aaef4bbf737b1`.
[Full remote CI](https://github.com/complynx/zns-chatbot/actions/runs/39536248531)
passed 835 integration roots and 357 UI tests, with the complete job log retained.

Telegram-backed drafts now retain source references and download originals when
needed. Browser uploads and generated JPEG receipts retain their bytes. An
unavailable Telegram source produces an error asking for a new photo. Deleting
a chat message alone does not prove the Telegram file has become unavailable.

## Upgrade and preservation

Four independent controller review passes closed five verified findings; the
last pass had no actionable findings. The upgrade fenced ingress, made a fresh
backup, migrated, replaced the bot, verified it, resumed ingress and verified a
fresh polling cycle. Each phase reached its recorded terminal success.

The custom PostgreSQL backup is 2,980,822 bytes and was fully decoded with
`pg_restore --file=/dev/null`. This verifies archive readability, not a restore.
Old bot and poller logs were archived before replacement. Migration 68 was run
by the exact candidate image in a separate migration container, which was then
removed. There were zero primary drafts before migration; this installation
does not prove conversion of populated primary drafts.

Only the bot container was recreated. The original poller container was stopped
and started; the other nine runtime fingerprints were unchanged. Database
snapshots preserved domain rows and non-target schema. Checks allowed the
expected migration metadata, River heartbeat changes and default-age terminal
job cleanup; they did not permit arbitrary queue changes. Original queued work
and Loom run contents, IDs and states were preserved.

Docker stop command elapsed times were 0.390 seconds for the poller and 0.641
seconds for the bot; both exited normally. These are CLI observations, not exact
application drain latency measurements. The replacement was running and ready
before ingress reopened. A fresh successful polling cycle was observed from the
resumed original poller. No Telegram messages, IAM grants or new Loom runs were
created by the upgrade.

## Installed HTTP checks

All 18 public Mini App assets matched checked-in bytes through the installed
primary HTTP endpoint. Responses had `Cache-Control: no-store` and `nosniff`.
Unauthenticated requests to `/app/me`, `/app/avatars/history` and a draft-original
endpoint returned 401. Runtime fingerprints, schema, data, queues and Loom state
were checked before and after these reads.

The reviewed read-only observer v36 pins the accepted checkpoint, installed
inputs, helper hashes, image, all 11 runtime fingerprints, settings, database
metadata, schema and manifest, then checks data and Loom preservation. It passed
against the installed primary. It permits unrelated development-tree changes;
it does not substitute current source files for evidence of installed code.

## Evidence and remaining limits

Private local evidence is under
`.cache/shuttle-work/legacy-avatar-migration-v1/primary-upgrade-run-v1/`:
`checkpoint.json`, `pre-upgrade-log-archive.json`,
`installed-http-assets-v1/proof.json`, `installed-epoch-v36.json` and
`installed-observer-v36-proof.json`. Controller review records are in the parent
directory. Dumps and runtime logs remain private and are not committed.

The separate [historical-reference runtime report](shuttle-avatar-history-runtime-qa-2026-10-21.md)
covers controlled-provider and browser workflows. The mock Telegram proxy's
privacy-header fix is separate commit `c4d3d2b4`; its own CI result is tracked
separately from acceptance of the installed bot source.

This installation does not establish a real legacy export/import, a fresh
end-to-end authenticated workflow on the primary, real Telegram WebView or joint
ZitadelTG login acceptance. Forward migration on snapshot clones and a rolled-back
Down SQL probe do not establish a full Goose downgrade or old-binary rollback.
Production was not changed and production readiness remains unproven.
