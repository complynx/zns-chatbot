# Protected registration details: Mini App backend

The working tree implements private legal-name and passport-number forms and
their transactional Mini App backend. Native event registration now passes the
scoped [Telegram/form API proof](shuttle-registration-details-native-qa-2026-10-16.md);
the later [solo prerequisite matrix](shuttle-registration-details-paths-qa-2026-10-16.md)
also covers event-specific agent/mixed routes with a manual provider. This follows the [schema36 conversational rollout](shuttle-conversational-runtime-qa-2026-10-16.md).
The owned stand now runs [candidate ebd1bdf](shuttle-registration-details-native-qa-2026-10-16.md)
on schema37 with its local protection
key enabled. Bot-only activation and mixed form/agent runtime acceptance passed
with real IAM/Telegram/Loom and a manual provider. Trusted Telegram WebView,
unattended model interpretation and remaining registration/pass workflows remain open.

## API

Routes are registered only when the existing registration-details protector is
configured. They use the same verified Telegram Mini App session and server-side
IAM verification as the [profile form](shuttle-profile-entry.md). No IAM redirect
or caller-supplied owner ID is accepted.

- GET /app/registration-details reads current version and presence flags.
- GET /app/registration-details/operations/{operation} reads an immutable owner
  receipt. A missing receipt returns404 for a current session; an unavailable
  session returns401. A receipt lookup does not submit or retry a write.
- POST /app/registration-details/legal-name saves one legal-name value.
- POST /app/registration-details/passport-number saves one passport-number value.

Each POST accepts operation_id (nonzero UUID), expected_version (required integer)
and value. Unknown fields and invalid values return400 with a fixed error.
Mutations return409 for a stale version or reuse of an operation ID with a
changed canonical request. Retry an uncertain write with the same operation ID,
version, field and value. Successful replay returns the original receipt even
after newer edits. Read current status separately after recovering that receipt.

Success bodies contain only version, legal_name_present and
passport_number_present. Stored private values are never read back or prefilled.
Cache-Control is no-store; POST requires the configured Origin and JSON content
type. Without a protection key the routes are absent: GET returns404 through the
existing page fallback, and POST returns405 from the existing method router.

## Transaction and history

The public API verifies IAM before entering the business transaction. The store
then locks the owner, locks the session and checks its current binding,
revocation, generation and expiry. The shared agent/form mutation replays before
CAS, or encrypts the updated record, records a redacted operation receipt and
fulfills eligible persisted data expectations. The Mini App action journal is
written before the same commit. A journal failure rolls all these changes back.
Logout and local revocation serialize with this transaction.

This global owner operation does not lock events. It shares the encrypted record
and expectation fulfillment with the agent API. A legal name is distinct from
the display/profile name. Saving required fields does not create a registration,
assign a pass or express event/role/payment consent. Historical replay does not
fulfill newer expectations.

Migration37 adds shuttle_miniapp_details_actions. It stores only owner,
operation ID, field and completion time, with a foreign key to the redacted
operation receipt. A repeated form operation creates no second action or fresh
completion time. The next user-triggered agent task receives at most five recent
owner actions predating that task, with historical presence/version from their
receipts. This history contains no private values or session credentials and is
explicitly marked as context, not instructions or consent. A form write does not
start Loom proactively.

Down migration locks the journal before checking retained history. It refuses to
remove a nonempty journal, including an insertion committed while rollback waits
for the lock. Export retained history before attempting rollback.

## Component verification, 2026-10-16

The extraction was checked against the unchanged prior full Linux/race source
freeze, then the same16 neighboring behavioral scenarios passed after extraction.
Its independent textual review found no actionable findings.

The backend proof passed22 root scenarios /22 pass events on Linux Go1.27.2 with
race detection, real disposable PostgreSQL17 and mock IAM/Loom/Telegram. It covers
form-to-agent completion of one persisted expectation, exact retry after restart,
historical receipt versus current state, no proactive run or implicit registration,
wrong-owner access, stale/conflicting/invalid requests, origin enforcement,
logout, expiry during a database lock wait, atomic rollback on journal failure,
redacted reactive context, disabled routes, and concurrent migration retention.
The run includes16 existing details/request/profile scenarios. All810 frozen
release/test inputs and the ten main-service fingerprints stayed unchanged.

Independent source review found a migration rollback race. The lock and a
behavioral reproduction were added; a fresh full scoped review found no actionable
findings. Scoped API/db/task/integration lint reported0 issues. The initial QA
failure was only an incorrect404 expectation for disabled POST; its logs remain
preserved. The final accepted proof is ignored
.cache/shuttle-work/miniapp-details-api-v3-qa/. Containers and the owned volume
were removed with terminal0/noOOM. PostgreSQL cleanup uses SIGINT and is not an
application restart acceptance test. No commit, push or deployment was performed.

## HTML forms and recovery

The existing Mini App page now has RU/EN single-field forms with explicit Save
buttons. Both inputs start blank; the page reads only presence/version. Native
required validation blocks empty form submission, and Enter submits the focused
field's form. Saving or recovering one field clears that field without destroying
an unsubmitted draft in the other field. Both drafts and the in-memory submitted
body are cleared on logout attempts, session denial and reconnect. Inputs opt out
of autocomplete, spell checking and automatic capitalization; accessible
presence/help text is associated with each input.

Before a private POST, the page persists only owner, operation_id,
expected_version and field in sessionStorage. The value and complete request body
stay in memory. If this metadata cannot be persisted, no POST is sent. This is
separate from the existing profile form's nonprivate draft behavior. An uncertain
POST keeps an immutable in-memory body; the matching Save button repeats those
bytes with the same key. The other field cannot submit until the pending result
is resolved. Module disablement blocks continuation without discarding metadata.

After reload the page reads current presence and checks the operation receipt.
A successful receipt establishes the historical outcome; a separate read refreshes
current presence/version. Missing receipt404 with the same current version keeps
the original key and requires explicit reentry of its original value. The page
never invents or automatically submits a replacement operation. If a different
value is reentered while the original eventually commits, server request hashing
rejects conflicting reuse; receipt recovery reports the earlier saved request.

A missing receipt observed after a newer monotonic owner version means the old
expected-version CAS cannot apply. The UI reports that conflict and retires its
recovery pointer. It sends no new write; any later save requires a new explicit
user action using the current version. Lookup failure or an inconsistent version
retains the pending pointer. Validation/conflict responses are reconciled through
receipt/status reads before further input is enabled.

A new Telegram launch preserves only safe pending metadata until the server
identifies the owner. The same owner can recover; a different owner never queries
that earlier receipt. Logout retains the pointer for a later same-owner login,
while clearing private drafts immediately even if logout temporarily fails.

## UI component verification, 2026-10-16

The Node browser-behavior suite passes30 scenarios, including14 private-form
scenarios. These intercept all storage writes and check that synthetic private
values never enter them. They cover exact unknown-result retries, historical
receipt versus current state, explicit same-key reentry, stale CAS recovery,
lookup outages, missing storage, session denial, owner/launch changes, disabled
module, unrelated draft retention, and logout failure. The16 preexisting session,
profile and account scenarios remain green. CI already runs this test file.
Evidence: ignored .cache/shuttle-work/miniapp-details-ui/node-tests-v3.log and
source-freeze-v3.json.

A separate bounded Linux Go1.27.2/race gate passes27 roots /27 events: the22
backend/neighboring scenarios above, four Mini App identity/session scenarios,
and the public assets/CSP route test. It uses real disposable PostgreSQL17 and
mock IAM/Loom/Telegram. All810 release/Go-test inputs stayed frozen; the UI script
and its Node tests have a separate four-file freeze. All ten main-service
fingerprints stayed unchanged. Evidence: ignored
.cache/shuttle-work/miniapp-details-ui-wire-qa/. Exact labelled containers and the
owned volume were removed with terminal0/noOOM; this cleanup is not application
restart acceptance.

Manual browser QA uses a loopback-only fixture API and a non-authorizing Telegram
SDK fixture. Production app.js and CSS are served directly. The HTML differs
only by replacing Telegram's SDK URL with the local fixture script. No real IAM,
Loom or Telegram authentication is claimed for this browser run. The actual DOM
confirmed required validation, Enter, both-field draft retention, retries with a
disabled submitted input, recovery after a committed lost response, and explicit
original-key reentry after an unaccepted request. Six POST attempts produced four
receipts; both duplicate attempts retained the original operation and body hash.
Final inputs were blank and saved values hidden.

The core browser run retains its original asset hashes. Final small CSS,
accessibility and logout changes were then reloaded and checked separately:
full-page visual inspection, associated hints, and clearing/hiding both drafts on
logout. Browser read-only evaluation does not expose sessionStorage, so direct
real-browser storage inspection is not claimed; storage privacy is supported by
source review and Node interception. Geometry from the reconstructed DOM was not
used as layout proof. Screenshots were displayed inline; there is no saved image
artifact. Evidence: ignored
.cache/shuttle-work/miniapp-details-ui/browser-v2/core-acceptance.json and
completed-core-state.json. The fixture process and temporary browser tab were
removed after acceptance.

Independent privacy review was clean. Usability review found that a successful
single-field save cleared the other draft; this was fixed and covered behaviorally.
Fresh complete reviews were clean, including the final logout/CSS/accessibility
changes. This UI component gate performed no commit, push or runtime rollout;
the subsequent owned activation is described below.

## Image and isolated migration acceptance

Candidate image sha256:7645802278be3867dd84179b3daad04847bf0db9a67d458ac95118c7ba4ee3de
was built for linux/amd64 with the unmodified release Dockerfile. Its711 frozen
release inputs match the accepted behavioral-gate inputs. The dedicated builder
used2GiB/no swap, two CPUs and one concurrent build step. The image is retained;
the builder and its cache were removed. Evidence: ignored
.cache/shuttle-work/miniapp-details-images-qa/checkpoint.json.

The actual candidate binary migrated a fresh owned PostgreSQL snapshot from36
to37. Hashes of all74 preexisting application/runtime tables remained identical;
the new redacted action journal was empty. The clone used bounded tmpfs storage
and network=none; only its loopback database was reachable by the migrator. No
worker, IAM client, Telegram sender or Loom client was started. Windows bind paths
were canonicalized, mounted readonly and checked for changes. All ten live
service fingerprints stayed unchanged. The exact labelled clone and migrator
were removed with terminal0/noOOM and no persistent clone volume. Evidence:
ignored .cache/shuttle-work/miniapp-details-schema-qa/checkpoint.json.

The operational helper passed fresh independent review after fixing the
PostgreSQL temporary-server readiness race and adding separate failed-run cleanup.
The successful proof exercises normal cleanup; the abort path is source-reviewed,
not claimed as fault-injection acceptance. This image/snapshot proof preceded
the owned activation below and does not itself close real-service form acceptance.

## Owned activation checkpoint

The owned shuttle-live-test bot now runs the candidate image and schema37. The
poller and bot were drained/stopped; a fresh private database dump was verified
before migration. All74 old tables were preserved before startup; startup changes
were restricted to River lease/queue heartbeat fields, with names, creation time,
metadata and paused state compared against the backup. The empty journal was the
only new table. Settings, mounts, limits and security were retained, adding only
the optional local encryption key. Eight companions, including Loom, retained
their exact container/image/start/restart fingerprints. The same poller was
restored and completed a fresh successful cycle.

The first-activation guard checks both effective environment and semantic YAML
key presence, and refuses to replace a key or retained protected data/receipts.
The key is retained in a private service env_file referenced by the recorded
Compose override; later Compose commands need no extra secret CLI argument.
Keep this key with backups; never regenerate it after protected writes. No schema
Down or backup restore was performed. Unknown Compose outcomes are recovered by
observing the same candidate rather than recreating it.

A delegated real Zitadel owner token read both profile and redacted details with
no business mutations. The probe receipt is bound to the activation ID, exact
candidate fingerprint, key hash, subject, target and timestamp. This is an owner
API probe, not mixed-flow or trusted WebView acceptance. Evidence is ignored
.cache/shuttle-live-test/miniapp-details-main-cutover/checkpoint.json and its
owner-probe.json. Independent operational review findings were fixed before
mutation; the final complete review was clean. No production, commit or push.

## Mixed runtime acceptance and next gates

The [owned mixed-flow acceptance](shuttle-miniapp-registration-details-qa-2026-10-16.md)
passed real IAM/form APIs, Telegram ingress and Loom broker orchestration: form,
agent, form and agent continued the same owner record and run. It includes same-bot
restart/replay, stale-version refusal, redacted context and preserved business data.
A QA-only script/write transport error was retained and recovered through native
module_call in the same run. The provider was driven manually.

Trusted Telegram WebView entry still requires the separate CA/browser step.
The [scoped event-specific solo matrix](shuttle-registration-details-paths-qa-2026-10-16.md)
now passes with a manual provider. Remaining registration/pass menu parity and
unattended model interpretation remain open. Completing these global fields does not by itself
close registration/pass four-path acceptance.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
