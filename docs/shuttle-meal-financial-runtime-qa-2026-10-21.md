# Financial meals: bounded runtime acceptance, 2026-10-21

Status: bounded substage accepted on the owned meal-financial-r1 stand. The
mvp-89/schema69 source was not installed on the primary mvp-86/schema68 stand
at that checkpoint. Its subsequent [installation](shuttle-meal-primary-qa-2026-10-21.md)
and [controlled-agent payment QA](shuttle-meal-agent-payment-qa-2026-10-21.md)
are separate accepted evidence.
Native submission, actual Loom submission, native proof and ambassador confirmation
passed within the scope below. Runtime cross-owner/module denial also passed;
the full release gate was still open at this runtime checkpoint.

## Proven behavior

The candidate cross-builds both static Linux/amd64 Go1.27.2 binaries from frozen
source and packages them on the pinned owned runtime base. Image extraction
verifies both binary hashes. This uses the accepted base's runtime/licence
contents; it is not a fresh full release-Dockerfile or full-CI acceptance claim.

Actual headless Edge at a mobile viewport uses real bot HTTP APIs, PostgreSQL and
Zitadel. A simulated Telegram SDK supplies signed synthetic-user initData from
our mock Telegram. It is not real Telegram Web or a joint ZitadelTG check.

With this stand's Loom stopped, the user saves a complete meal draft, reviews
all frozen components, charge lines, total7.15BYN and the Europe/Berlin deadline,
then explicitly submits. The test drops the response after the real commit.
The page retains the exact request. Another client writes a newer draft; page
reload recovers the original receipt, displays one submitted order and retains
the newer draft. Reload sends no second submit. No Loom run or bot task is
created by this native flow. A separate recovery probe also preserves an earlier
committed order after a failed QA attempt; it performs reads only.

The next explicit synthetic chat request runs through real Loom and IAM. The
scripted counted provider calls zinusya_meals.get, operation and list. The prompt
contains earlier native financial receipt metadata and kind-aware instructions.
Reads return the current native order and original immutable submit receipt.
The same run completes and delivers a reply without changing either order or
creating payment evidence. This proves transport/authorization integration, not
autonomous model reasoning or a native provider's quality.

Normal SIGTERM stops only the candidate bot with exit0 in0.656seconds measured
around docker stop, including CLI overhead. Restart preserves orders, operation
receipts, preparations, meal drafts and evidence tables exactly. The original
submission receipt remains readable. This is an idle completed-task restart;
pending financial-agent recovery is not proved by this check.

## Agent submission and pending consent restart

A second actual Loom run saves a complete draft and prepares a meal submission.
The scripted provider receives the full saved human consent text. Telegram
delivery includes every meal component, charges, total and event timezone.
Before consent, no order or payment evidence exists for this operation.

Only this stand's bot is restarted with SIGTERM while consent is pending. It
exits normally in0.687seconds, including Docker CLI overhead. The same task, run
and pending approval survive. Plain-text user approval resolves that approval;
the exact original arguments create one order. The original run then finishes.
This proves the pending-consent restart path, not every possible failure window.

## Consumed draft correction and payment review

Real browser QA found that a submitted draft retains its version and catalog
revision but has no evaluation. The form incorrectly rejected that valid cleared
draft, hiding submitted orders. The corrected form shows order history, uses
the current menu for new choices, and retains the draft revision for the next
save. The UI fixture now matches the server's consumed-draft response. The old
code fails the corrected case;366 UI cases and the Go API package pass after the
fix. Independent source review found no remaining actionable finding.

The corrected image replaces only this owned stand's bot. Financial rows and
the other eight service instances remain unchanged; primary readback is preserved.
The browser then opens the existing agent-created order, selects an accepted
Telegram photo, loads its original68bytes and verifies the SHA256. Direct and
proxied HTTP reads independently match. One native proof submission creates one
pending meal claim for7.15BYN. It creates no capacity allocation or additional
Loom run, and does not confirm money.

The fixture administrator opens that claim in a separate real browser session.
Preparing a saved decision leaves the evidence row unchanged and confirmation
disabled until the proof is loaded. A fresh session reads the saved decision,
loads and checks the same file, then explicitly confirms it. The evidence becomes
confirmed at version2, with7.15BYN credited and no meal capacity allocation.
These checks use synthetic users and funds on the owned stand.

## Runtime access checks

A second user receives an assigned unpaid pass for the same event. The owner
can read the submitted order and proof; the second user cannot read that order,
its operation receipt, payment evidence or file. The operation API reports
HTTP200 with status not_found and no result; other denied reads return404.

With meals disabled and the service-order module enabled, the original owner
cannot read the meal order, operation receipt or evidence. The service-order
tool also cannot expose the meal order. Restoring the original event settings
restores the owner's meal read. Orders, evidence, operation receipts,
preparations and meal drafts remain byte-for-byte equal as JSON rows across
these module checks. Event versions advance normally for the settings changes.

## Retained failed probes

The initial fixture read missed the agent response's result envelope. Identities
and events were retained; only deadline configuration continued. The first
browser's second-client request used Bearer instead of ZinusyaSession and got401
after its intended order had committed. That order was recovered without repeat
submission. A recovery assertion ran before the asynchronous order-list render;
an explicit render wait passed. The old generic tool-output helper stripped an
operation receipt's own result field. The raw Loom output was correct; the
original run continued rather than receiving a new request. Application source
was unchanged for these QA fixes. All original failed JSON/logs remain ignored.

Later failed probes are also retained. The agent-submit QA read the SQL storage
envelope as a public order after a successful commit; it resumed the same run
without repeating the submit. Browser proof probes exposed the cleared-draft
product bug described above. A reused initData was correctly rejected with409.
Subsequent probes included an empty Playwright response-body observation, a
CSP-blocked diagnostic fetch of a blob URL, a missing await in the diagnostic
Blob read, and a click that produced no HTTP write. The cause of that missed
click is not established. The final browser probe checks the actual application
Blob and form readiness and passes; earlier probes sent no payment claim. No
file-download product change was made on this evidence.

The isolation QA initially expected receipt denial to use HTTP403/404. Its
actual contract is HTTP200 with status not_found. Both original failed probes
are retained. Read-only checks resumed with assertions on the envelope; the
second user's registration and pass were not created again. Event settings
were restored after both module-check attempts.

## Remaining acceptance and resource state

The following full-release attempt found another issue during combined source
review: updating event metadata or publishing a new menu left an older saved
draft that could return503 during preparation and lock the native form's unknown
request. A behavioral HTTP test reproduces both cases. The snapshot boundary now
returns409 for stale event/catalog lineage, while preserving genuine corrupt-term
errors as server failures. The same HTTP paths pass after resaving the draft, and
the UI conflict/reload/resave/confirm path passes. The UI suite now has367 passing
cases; an independent focused review has no actionable findings.

The first full race gate was explicitly interrupted during inventory after its
unit stage passed. It is not full-gate acceptance. Its logs, source, database and
red/green evidence are retained under release-v1/; a new release-v2/ gate runs
against the corrected source. The first complete Dockerfile build also passed,
but that image predates this correction and is not the final candidate.

The corrected candidate's first complete-image attempt hit the separate builder's
1.5GiB limit: the API compiler was killed and the cgroup recorded an OOM kill.
That failed attempt is not accepted. Logs and counters are retained, and its
builder is removed. A fresh3GiB builder successfully builds the unchanged release
Dockerfile and is then removed with its cache. The corrected full image remains
local; the independent PostgreSQL/race gate continues on its existing frozen source.

The [separate schema69 rehearsal](shuttle-meal-schema69-rehearsal-2026-10-21.md)
also passes on a restored primary database copy, including a clean forward,
no-op, rollback and forward sequence. Its resources are archived/removed.
This is migration-copy evidence; the primary still ran schema68/mvp-86 then.

The later [application release check](shuttle-meal-release-qa-2026-10-21.md)
passes864 integration roots /1293 cases and367 UI cases. It retains a post-test
fixture shutdown timeout and separately verified retirement. Subsequent remote CI
and primary installation are accepted in the installation report linked above.

Still open: broader failure-window recovery.
Exact original native-preparation adoption is a separate
documented upstream contract gate. No production readiness or deployment is claimed.

The nine owned services, their network and volumes are removed after exporting
both databases and all container logs. Each dump passed pg_restore --list.
The temporary packaging probe and every Edge context are closed.
The main stand's domain and runtime readback remain unchanged. At this checkpoint
the archived database contains three synthetic orders, one confirmed meal proof
and no pending task. Only this owned QA project was removed; no global prune ran.

Private source hashes, checkpoints, transcripts, screenshots and original failed
probes: .cache/shuttle-work/meal-financial-flow-v1/runtime-v1/. No secrets are in
this report. Candidate upgrade and resumed browser evidence are in the sibling
runtime-fix-v2/ folder; the source fix proof is in consumed-draft-fix-v1/.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
