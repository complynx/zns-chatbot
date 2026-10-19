# Mini App action-context owned QA, 2026-10-16

The action-context acceptance used owned shuttle-live-test image
`sha256:2c5618e3df2cf721205a70d5a5f5cea105fc6632dbce4979efc19af276bf0a61`
and schema34. The stand subsequently moved to structured-registration image
fffc16debed6 on the same schema; this report describes the earlier acceptance.
This is local runtime acceptance, not production readiness. There
was no commit, push, publication, production change or change to a Loom developer
checkout/stand.

## Build and additive migration

The complete unchanged Dockerfile built682 frozen source inputs successfully;
image size is57,580,994 bytes. The dedicated builder had2GiB memory/no additional
swap,2 CPUs and one build worker. OOM/oom_kill counters were zero. memory.events
max880 is a limit-hit event count, not peak memory. The builder was intentionally
stopped (exit1/noOOM), then removed with its own cache; the image remains.

The reviewed cutover stopped the existing Telegram poller and bot, required drained
tasks/outbox, and saved a2,257,868-byte pg_dump archive with its hash and verified
pg_restore listing. Migration34 ran in the candidate image, exited0/noOOM and its
exact container was removed. All64 existing shuttle table data hashes matched;
only the new empty shuttle_miniapp_profile_actions table was added. These are
canonical JSON row hashes, not physical PostgreSQL file comparisons. Schema and
Goose history advanced as intended. Existing environment, settings, mounts,
resource/security limits and all eight companion instances were preserved. A
real-IAM owner profile read changed none of the65 application table hashes. The
same poller restarted and completed a fresh successful cycle. Private prelink
remains disabled; manifest service is zinusya, revision mvp-33 (tool contract
revision is independent of database migration number).

## Form to native Telegram and real Loom

The test used Daniel's authorized account/private chat8968148043 and test
bot6087685431. A proof signed locally with the owned test bot token authenticated
the existing owner through real IAM. It was not Telegram-issued initData and did
not establish a trusted WebView browser session.

The form APIs saved the distinct name Daniel QA MiniApp 20261016 and language ru.
Exact replay returned the original results; a stale-version language write was
rejected and produced no action record. The same bot container stopped0/noOOM and
restarted; the same form session and exact name receipt survived. There were two
journal entries and no new Loom run/input from these form operations.

A single actual Telegram Web message144 then asked what had changed in the form.
The actual real-Loom input included both exact operation UUIDs and the completed
Mini App action-history marker, excluded the rejected operation and session token,
and retained the current profile. A script read returned the correct owner/name/
language through real IAM. The manually supplied provider reply was delivered as
bot message145 and visibly reported the saved name, ru and two successful requests.
No profile or financial write was requested by that turn.

Cleanup used two more journaled form operations to restore Daniel QA/en. Profile
version advanced14 to18, no name expectation remained, and exactly four journal
records remained. Logout succeeded and the closed session then denied profile
reads. Seven business hashes stayed unchanged: passes, pairs, registrations,
payment evidence/reviews, events and settings. The bot kept its new same-container
restart fingerprint, and poller/companion fingerprints stayed unchanged throughout
this QA. Cleanup created no additional Loom run/input.

The provider was driven manually via the owned mailbox; reported token usage is
fixture data, not measured model usage. This proves the authenticated transport,
persisted action facts, restart/replay and next-turn context path. It does not prove
unattended model interpretation, trusted WebView rendering/login or full
registration/pass form parity.

## Evidence, reviews and remaining work

Ignored receipts are under .cache/shuttle-work/miniapp-action-image-qa/,
.cache/shuttle-live-test/miniapp-action-main-cutover/ and
.cache/shuttle-live-test/miniapp-action-native-qa/. The session/proof are private
ignored QA data and must not be copied into reports. Cutover and native QA helper
reviews independently reported no actionable findings. Scoped source race/lint
and prior review results are documented in
[the action-context contract](shuttle-miniapp-action-context.md); Go source stayed
unchanged during build and runtime acceptance and those tests were not repeated.

The Telegram response was checked in the live browser. A screenshot was captured,
but the browser tool could not save it because its filesystem returned EPERM. No
saved screenshot artifact is claimed; message IDs and the observed result are
recorded in the private checkpoint and tool transcript.

All ten main services are running. No temporary builder or migration/test container
remains. Trusted CA/browser entry and real zitadeltg linking remain deferred to
the user credential/trust step. Full structured registration/pass workflows,
agent reply-keyboard questions, orders and massage writes remain roadmap work.
For binary rollback, stop ingress and drain/reconcile accepted tasks/outbox first;
retain schema34 and journal data. Older binaries do not record new form actions.
Do not use a schema Down or restore a backup merely to change the binary.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
