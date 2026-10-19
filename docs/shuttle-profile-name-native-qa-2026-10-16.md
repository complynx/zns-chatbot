# Profile/name image and native QA, 2026-10-16

This report records acceptance of image
`sha256:0137733521b6da49d4dd4a18ad51e40673f90b88bf82520558baceeebf8542c9`.
It was later superseded by the [action-context rollout](shuttle-miniapp-action-qa-2026-10-16.md).
At this acceptance, schema remained33, manifest mvp-33 and private prelink remains disabled. This is
local acceptance only. No commit, push, publication or production cutover occurred.

## Image and cutover

The complete unchanged Dockerfile built from680 frozen source inputs. The bounded
builder had2GiB memory, no additional swap,2 CPUs and one build worker. Build
completed successfully with zero OOM events. The cgroup memory.events max counter
was1609 (limit-hit events, not MiB); no peak-memory measurement was retained.
The builder was then intentionally stopped (exit1/noOOM) and removed with its
cache. This exit is cleanup evidence, not a failed build.

Image-only cutover preserved all64 table hashes, configuration, settings, mounts,
resource/security parameters, companion identities and the existing poller. A fresh
backup was taken before cutover. Readiness, a real-IAM owner profile read and a
fresh successful poll cycle passed. No migration process ran. Static /app/,
/app/app.js and /app/app.css returned200 with no-store and exact source bytes.
That static probe does not prove trusted Telegram WebView authentication or forms.

## Native commands, restart and stale answers

Only Daniel's authorized account8968148043 and test bot6087685431 in their
private chat (chat_id8968148043) were used. With the owned Loom stopped, /name created a durable ForceReply prompt.
The same bot container was stopped and restarted with the exact pending raw
profile unchanged. A Telegram Reply to that prompt saved the distinct temporary
name Daniel QA Native 20261016. Another reply to the original prompt was refused;
the newer name did not change. /name Daniel QA restored the original name.
All four tasks completed and all replies were delivered without a Loom run/input.
Seven scoped business-table hashes stayed unchanged: passes, pairs, registrations,
payment evidence/reviews, events and settings. The profile advanced from version4
to7; an old expired raw expectation was replaced, not restored. Name/language were
restored and no live name expectation remained. The same Loom was restarted.

## Agent and mixed continuation

Eight further tasks covered agent request to native completion, native /name and
/menu to agent completion, full agent request/answer, and explicit native cleanup.
All completed with delivered replies. Agent tools used the roleless owner's real
IAM credentials, actual Loom script reads and module writes. Manual action facts,
profile versions and the exact shared expectation were present in the next genuine
user-triggered agent context. Commands did not create additional Loom runs/inputs.
Final name Daniel QA and language en were restored, no expectation remained, the
seven business hashes and all service instance fingerprints were preserved.

The provider was manually driven by Codex through the mailbox. Usage figures are
fixture values, not measured model tokens. These checks prove transport, owner
permissions, durable shared state and continuation; they do not prove unattended
model interpretation or complete registration workflows.

Two failed helper attempts remain recorded. First, a read sent through module_call
was correctly rejected with use_script_for_reads; the helper changed to script
and continued the same run. Later it sent a script call to a summary step. Loom
correctly recorded summary_failed and paused context_unavailable. Scoped step
inspection proved that no profile write had occurred. After independent review,
the helper distinguished summary/model steps and explicitly resumed the same run
with a persisted idempotency key. No Telegram input was resent and no replacement
run was created. The original failed summary and recovery receipt remain evidence.
This was a QA driver defect, not a demonstrated Loom bug. Review also found an
uncertain-resume replay gap; the saved key is now replayed after a lost response.
The final independent review reported no actionable findings.

Earlier native helper preflights also retained an expired-raw-expectation guard
failure and a zero-UUID route-target observation error. Both were corrected without
duplicate Telegram sends; separate fresh reviews passed. They were not product
failures and are not erased from the retained evidence.

## Evidence and remaining gates

Ignored receipts are under .cache/shuttle-work/profile-name-image-qa/,
.cache/shuttle-live-test/profile-name-main-cutover/,
.cache/shuttle-live-test/profile-name-native-qa/ (initial preflight),
.cache/shuttle-live-test/profile-name-native-retry-qa/ and
.cache/shuttle-live-test/profile-name-mixed-qa/. Source race/lint scopes and earlier
failed test attempts remain documented in the two component documents. No Go
source changed during this runtime QA; those checks were not broadly rerun.
All ten owned main services are running. No temporary builder or test Go/PG
container remains. A memory snapshot showed the largest service using122MiB;
this is one observation, not a load-test claim.

Trusted Mini App writes, real zitadeltg/browser linking, transactional Mini App
manual-action facts, full structured registration/pass workflows and agent reply
keyboards remain open. CA trust and real Telegram OIDC credentials still need the
deferred user step. Older-image rollback requires stopping ingress and reconciling
accepted tasks/outbox first; the older binary cannot process these new routes and
saved payloads. Do not roll back schema or overwrite the database to change images.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
