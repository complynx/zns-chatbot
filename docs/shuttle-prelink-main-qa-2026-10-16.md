# Owned main bot image/schema33 cutover — 2026-10-16

The existing `shuttle-live-test` bot now runs the reviewed immutable image
`sha256:2caee3da1ac4fa77d75b3e822438954d5c063b1a3faee717ecdb427897dbe06a`
and application schema33. The private prelink endpoint remains disabled; no
provider service or new live linking policy was activated in this stage.
Real Telegram Web Login credentials, browser trust and final IAM session-cookie
relay still require their separate interactive acceptance. This is an owned local
test-stand cutover, not a production release or full registration/UI acceptance.

## Execution and preservation

The complete current Compose model was resolved from the actual bot container's
configuration labels, including existing Loom/resource/image overlays. Its only
proposed model change was the bot image. Current runtime environment matched the
resolved model plus image defaults; no secret value was printed. All672 recorded
bot source inputs still matched the accepted image build. The root index stayed
empty. An independent textual review of the rollout harness found no issue.

Ingress stopped first. After verifying zero active application tasks, the old
bot stopped cleanly; both final stops exited0 without OOM. A fresh custom-format
PostgreSQL dump was taken with both stopped:2,235,394 bytes, private SHA256 receipt
and a readable pg_restore catalog. It was not restored to the main database.
The earlier [restored-copy migration/old-image gate](shuttle-prelink-images-qa-2026-10-16.md)
is a separate proof, not a restore test of this new dump.

The new image's explicit migration process exited0/OOMfalse and applied32→33.
All63 existing shuttle-table content hashes matched before/after migration,
startup and the owner API read. Only `shuttle_telegram_prelinks` was added and
remains empty. The migration container finished normally and was removed by
its verified owned identity. No schema Down or user/account merge occurred.

Only the main bot container was recreated. Its exact environment, bind mounts,
user, entrypoint/command, resource/security/port settings and network membership
matched their saved values. Eight companion containers retained their IDs,
images, start times and restart counts. Configuration file hashes matched.
The same poller container was stopped/started, with no restart-count increase.
A fresh successful polling cycle and stable running state were observed before
acceptance. Bot and Loom readiness both returned200. The private manifest reports
`service_id=zinusya`, revision `mvp-33`; exact prelink POST returns404 while disabled.

A real local-IAM machine token and delegated owner token were obtained without
exposing credentials. The authorized owner profile read succeeded against the
new bot image and changed none of the64 shuttle tables. No Telegram message or
new Loom run was submitted by this QA. A polling cycle alone does not establish
an end-to-end user workflow.

## Retained failures and recovery

The first attempt stopped before migration: the Python test poller exceeded its
five-second shutdown allowance and was killed, exit137/OOMfalse. Its old handler
only set a flag during HTTP waits. The narrow fix, red/green subprocess proof,
11-test guard suite and independent review are recorded in
[poller shutdown QA](shuttle-test-poller-shutdown-qa-2026-10-16.md).
The original failure remains in ignored `prelink-main-cutover/` evidence.

The second attempt migrated and recreated the bot once, then its verification
harness rejected equal mounts because in-memory tuples differed from lists loaded
from JSON. The failed checkpoint and harness were copied before correction.
The corrected comparison and separate verification phase resumed against the
same recorded running bot; no repeat migration, restart or recreation was used.
Review found a possible stale-checkpoint overwrite on a verification interruption;
reloading the latest saved checkpoint before recording failure fixed it. A fresh
review found no actionable finding. These are harness failures, not passed first
attempts or product rollback failures.

## Rollback boundary and private evidence

Ignored `.cache/shuttle-live-test/prelink-main-cutover-attempt2/` contains the fresh
dump/catalog, configuration hashes, table hashes, immutable image/container
receipts, failed-verification checkpoint, final acceptance and an old-image
Compose override/argument list. The old accepted image is
`sha256:8323e6f63d4b2a8967527378ea5dbff9eb7f7fa84076071ef528f64327040c7a`.
Before any rollback, inspect actual state, stop ingress, drain tasks and stop the
bot; validate configuration drift before applying the saved image-only rollback
command. Keep schema33. The earlier old-image gate proves startup/readiness on33,
not all old APIs or recovery of new prelink receipts. Never force-drop receipts
or restore a stale dump over subsequently accepted work. No rollback was needed.

The ten normal owned stand services remain. Temporary QA and migration containers
were removed without global prune. No Loom developer stand/checkout, production,
commit, push or merge was changed. Next: reviewed main provider/backend activation
and real interactive login when credentials/trust are available; independently,
shared forms and complete structured/agent/mixed workflows remain required.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
