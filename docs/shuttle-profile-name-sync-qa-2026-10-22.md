# Structured profile names: source and isolated QA, 2026-10-22

This checkpoint covers the candidate `mvp-92/schema71` source on `codex/shuttle`.
The accepted primary installation remains `mvp-91/schema70`; no new application
image or migration was installed there. First-use onboarding, trusted reuse of
existing IAM names, the Telegram `/name` agent-dialog port, and real-IAM
name-update acceptance remain open.

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
The primary application and the separate base IAM stand were not changed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
