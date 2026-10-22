# Structured profile names: source and isolated QA, 2026-10-22

This checkpoint covers the candidate `mvp-92/schema72` source on `codex/shuttle`.
The accepted primary installation remains `mvp-91/schema70`; no new application
image or migration was installed there. Real-model first-contact/name-dialog
acceptance and installed worker acceptance remain open. New `/start` messages
without a filled local profile and new `/name` messages use the agent dialogue; see the
[name-entry contract](shuttle-telegram-name-entry.md).

## Accepted behavior

- Profile tools `set_name` and `complete_name` and Mini App name writes require
  separate `first_name` and `last_name`. Components may contain spaces. The form
  uses separate labeled inputs in English and Russian. Older full-name strings
  remain visible; neither migration nor form guesses their split.
- A name change, original operation receipt and desired IAM-sync version commit
  together. A storage failure rolls all three back. Historical receipt replay
  cannot restore an older name or schedule it again. Language changes and
  repeated names do not create another name revision.
- A form can complete a name expectation created through the agent API. An
  agent API correction is visible through the form. Replaying the earlier
  form request returns its historical receipt and preserves the newer profile.
- The existing River service discovers durable sync work after a missed enqueue
  or application restart. IAM outage retains the intent. Early queue retries
  snooze through the persisted backoff instead of completing pending work.
- Dispatch checks local ownership/revocation and the exact active remote
  subject, organization and verified synthetic email. Its PATCH contains only
  given and family names, and success requires matching read-back. Unknown
  responses remain errors. A later read can reconcile an already-applied write
  without writing again.
- The worker serializes local name edits and admitted dispatch. Periodic
  ten-minute verification also repairs a timed-out older HTTP request that
  finishes after a newer name was acknowledged. This proves eventual repair,
  not atomic remote fencing. Once explicitly saved, the structured local name
  is authoritative for synchronization. No legal registration fields are filled.

## Verification performed

### Real owned IAM adapter and durable-store acceptance

A fresh synthetic user in the owned local Zitadel organization passed real
name read/write through the production adapter and durable PostgreSQL store.
The probe persisted a failed attempt during an injected client-side network
outage, reopened the store, saved a newer compound name and dispatched the
latest desired version. IAM read-back and the stored acknowledged version
matched; the older immutable operation receipt remained unchanged.

The user's synthetic email, username, one provider link, nickname, display
name, preferred language and gender remained equal to their baseline. The
grant list remained empty: this proves no grant was added, not preservation
of populated grants. The independent controller review found no actionable
issue. PostgreSQL independently reported migration 72; the Go probe's schema
label alone was not used as migration proof.

The probe used verified local HTTPS and only the owned fixture organization.
It made zero real Telegram authorizations and zero paid model calls. The
outage was injected in this client's transport; Zitadel and other services
stayed running. Reopening a store is not an application-process or River-worker
restart test. The primary stand was not upgraded by this probe. Its isolated
controller and receipts are retained under ignored QA evidence at
`.cache/shuttle-work/name-real-iam-20261022-v1/`.

After this probe, the combined profile/onboarding/prelink integration gate
passed in 156.313 seconds on isolated schemas. It covered first start, IAM-name
provenance/import, conversational name input, profile APIs/forms, prelink and
the named-user native entry paths. The candidate also compiled for Linux/amd64
with CGO disabled. Neither check installs the application image.

### Existing IAM name adoption

Migration 72 records whether this application created an IAM account with
technical name hints or found an existing verified account. Existing local users
are backfilled as `unknown`; a missing provenance row is also untrusted. Stop
the old application before applying this migration, then start the new binary:
old provisioning code cannot record the new provenance. Rollback refuses to
discard newly recorded provenance until it has been exported.

All provisioning callers must record technical intent before remote creation.
Recovering an unknown create result never changes that intent to `existing`.
Only an explicitly recorded existing account can supply an initially empty local
profile. Missing name components, known provisioning fallbacks and duplicate
components require explicit onboarding instead. Legacy full-name strings remain
intact and are not automatically split.

The IAM read holds no PostgreSQL connection. Before saving, the importer checks
ownership, revocation, provenance and the original profile version again. It
preserves an active name expectation and any intervening profile mutation. A
temporary IAM outage leaves adoption retryable and permits already-authorized
dialogue using the local profile. Imported names do not schedule a redundant
write back to IAM; later explicit edits use the durable synchronization above.

The isolated provenance/import selection passed in 21.610 seconds against
PostgreSQL and HTTP peers. It covers lost-create provenance, migration rollback,
single-connection reads, concurrent edits/revocation, a name dialog started
during the read, fallback names and Mini App bootstrap. A first textual review
found the dialog race; it was fixed and the second review had no actionable
findings. A broader integration run then exposed a concurrent-first-login
failure: another entry point could complete binding before provenance recording.
The guard now preserves completed provenance and permits a delayed create only
for the same immutable subject with a previously recorded technical intent.
The original failed runs remain failures, separate from repeated verification.
Five consecutive concurrent-login tests passed in 22.111 seconds. The final
provenance/import/concurrent-login selection passed in 14.414 seconds after a
guard-helper extraction; the broader profile/Mini App/prelink selection passed
in 76.741 seconds before that extraction. Identity, task and API unit packages
passed. The scoped linter and final DB lint reported zero issues, and sqlc
generation was stable. The final independent textual review had no actionable
findings. The owned PostgreSQL container stopped with exit 0 and OOM false and
was removed with its anonymous volume.
This does not prove real-IAM name writes or first-contact dialogue.

### Structured name and form checkpoint

The first-start selection passed in 37.585 seconds: new-user dialogue across
restart, explicit owner-tool completion, subsequent native `/start` during Loom
downtime, duplicate delivery after the profile changes, profile completion while
the original request waits for Loom, verified existing-IAM name reuse, and the
existing native avatar/pair/payment entry paths for already named users. Initial
test failures came from total-message counting and disabling Loom before the
original parent run completed. The final tests wait for the specific outbox
delivery and original run completion. They do not treat scripted provider output
as a model's name assessment.

The later agent-routed `/name`/navigation/profile-prompt integration selection
passed in 53.540 seconds. Task and API unit checks passed, and the scoped linter
reported zero issues. Independent review found no code defect; its outdated
documentation line was corrected. These tests use a scripted Loom provider and
explicit owner API calls. They verify that command text, Telegram suggestions,
unrelated messages and reply references cannot independently authorize a name
write. They do not verify a model's name judgment. The separate owned dialogue
PostgreSQL fixture was retired with exit 0 and OOM false.

The tests used an isolated PostgreSQL 17 container and HTTP Telegram, Loom and
IAM peers. Go package checks covered identity, DB, task, API and observability.
The profile/Mini App integration selection passed after the input port in
53.471 seconds. The preceding full DB package check passed in 51.671 seconds.
The current frontend suite passed all 378 tests. The configured linter reported
zero issues; sqlc compile/generation was stable and `git diff --check` passed.

Representative commands, with an isolated test database configured:

```sh
go test -p 1 -parallel 2 ./internal/identity ./internal/db ./internal/task ./internal/api ./internal/observability
go test -p 1 -parallel 2 ./integration -run 'Test(Profile|StructuredProfile|MiniAppProfile)' -count=1
node --test tools/test-*.cjs
sqlc compile
sqlc generate
```

Manual browser QA used the current HTML/CSS/app.js in the in-app browser, with
a disposable loopback HTTP peer and synthetic initialization data. Other modules
and the real Telegram SDK were omitted from that browser fixture. A name save
committed at the peer and returned 503. Reload retained both compound name
components and the original operation. Retrying sent the exact same body and
operation ID and produced one name change. Selecting Russian updated the form
labels. This is a browser/form proof, not real Telegram or IAM acceptance.

Independent textual reviews found and corrected two recovery defects: premature
completion during IAM backoff, and permanent divergence after a late older
remote write. Subsequent source reviews and the input/UI review had no
actionable findings. Nothing was staged during those review loops.

## Retained failures and cleanup

The first disposable PostgreSQL had a 256 MiB limit. A broader DB package run
omitted the test-parallelism limit, caused OOM, and failed. Its logs/state are
retained in ignored QA evidence. It was replaced with a bounded fixture using
384 MiB, 32 MiB shared buffers, 2 MiB work memory, 40 connections and explicit
Go package/test concurrency limits. The repeated gates passed without OOM.

The broader DB run also exposed an existing proof-expiry test that waited on
the Windows process clock while production expiry uses database time. The test
now waits for the PostgreSQL clock to cross the proof deadline. Its original
failed result remains recorded separately from the successful rerun.

The earlier disposable name-sync PostgreSQL did not stop within a five-second
cleanup timeout and exited 137, with OOM false. That is retained as a cleanup
failure; it is not a passed graceful PostgreSQL test or an application restart
claim. It contained synthetic QA data only and was retired after the tests.
The main application's graceful-restart contract was not broadened by this QA.

No production data, real Telegram authorization or paid model calls were used.
The primary application installation and base IAM service configuration were
not changed. The real-IAM probe created and updated a synthetic user and its
provider link within the owned mock organization; these fixture records remain
for diagnosis.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
