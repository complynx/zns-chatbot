# Native order transactions and reactive context

The candidate now has native session wrappers for the same order preparation,
submission and cancellation code used by the
[agent API](shuttle-orders-submission-api.md). This component implements the
store/session and recent-action parts of the
[agreed plan](shuttle-orders-submission-plan.md). The native
[HTTP](shuttle-miniapp-order-submission-http.md) and
[UI](shuttle-miniapp-order-submission-ui.md) components now have separate gates.
Native-only actual Mini App acceptance later passed; agent/mixed real Loom
handoffs remain pending. See `shuttle-orders-submission-real-qa-2026-10-17.md`.

## Session and shared business transactions

`PrepareMiniAppOrderSubmission`, `SubmitMiniAppOrder` and `CancelMiniAppOrder`
accept a deployment and a `MiniAppOrderSession` containing bearer hash and key
generation. They derive the owner from the session. Callers must verify IAM
before entry; the native HTTP layer now performs that check. The wrappers
introduce no new database table, workflow log or dependency.

The transaction first resolves the bearer without row locks, takes the same
owner-operation advisory lock as agent order calls, then locks event, owner and
session. It checks session state/expiry/key generation, current account identity,
bot/organization, enabled module and assigned-pass access before any receipt
replay or effect. Logout and account revocation serialize on the owner lock.
A write waiting for the event lock rechecks the session after that wait. No
IAM, Telegram or Loom call occurs while these locks are held.

Each wrapper calls the existing private transaction helper and commits only a
new successful write. Matching receipt replay rolls back its read-only
transaction. Prepare/cancel validation and request hashes are shared with the
agent entry points. Submission uses the same exact-proposal hash and canonical
owner/event/source-draft identity. The original business result stays immutable
across transport aliases, cancellation and a reopened store. A new Mini App call
records its trusted source as `miniapp`; replay of a receipt originally created
by another channel does not relabel that receipt or invent a native action.

Order, consumed draft and operation receipt remain atomic. Receipt failure
leaves no submitted order, no submission receipt, and no consumed draft. The
existing preparation remains usable after the failure is removed. No capacity
or confirmed money is created by this stage.

## Reactive action projection

`RecentMiniAppOrderActions` reads at most five existing native operation receipts
for this task's owner, newest first. It filters current bot/organization, module
and assigned-pass entitlement and excludes receipts whose timestamp is not
before the task's creation timestamp. A foreign task owner receives no history.
The projection contains only operation/event IDs, phase, optional proposal/order
IDs and UTC `recorded_at`; it does not include item labels, amounts, evidence,
profile details or session credentials.

`recorded_at` is the receipt insertion timestamp, not a promise of commit-order
linearization. The cutoff is the stored task timestamp. Later receipts do not
change that earlier task's window, but this bounded history is not a complete
workflow log and is not a current-state snapshot.

The profile context appends this metadata only when processing an explicit user
request. Native preparation, submission, cancellation and aliases do not start
Loom runs or Telegram deliveries. The prompt labels history as untrusted data,
not instructions or consent. It directs the agent to read the exact immutable
operation receipt and current order/draft separately. A preparation is not proof
of submission; history does not authorize another write, cancellation or clearing
of newer quantities.

## Behavioral evidence and limits

The broad gate passed 55 integration roots/68 test-pass events and all 13 task
roots/42 events: 68 roots/110 events overall, zero failures and zero skips. It
used real disposable PostgreSQL on Linux with the race detector and mock
identity/Loom/Telegram peers. Besides all earlier order/store/API cases, it
covers native-to-agent and agent-to-native preparation handoffs, reopened-store
recovery, session validity before historical replay, logout/expiry/key changes,
foreign owner/organization denial, logout while a submission waits, receipt
failure rollback and bounded reactive owner/module-filtered context. Existing
native selection, profile-language, restart/upstream-outage, revocation and
conversational critical paths were included.

After the broad pass, root review strengthened only the test's lock observation:
it now checks that a backend in this fixture database is blocked specifically by
the transaction holding the event lock. The original predicate could observe an
unrelated wait in another parallel test. The production-source hashes remained
identical; the manifest delta contains only
`integration/miniapp_order_submission_test.go`. All five new native roots/10
events then passed again with Linux/race. This is a focused test-strengthening
repeat, not a second 68-root run against the final test file.

Evidence is retained in `.cache/shuttle-work/orders-native-store-v1-qa/` and
`.cache/shuttle-work/orders-native-lock-v2-qa/`, including source manifests,
source delta, process state and cleanup receipts. Both used 870 frozen inputs.
The only initial lint finding was a test-only explicit struct conversion;
it was fixed before the broad frozen run. The final scoped lint reported zero
issues. Host compilation passed before behavioral acceptance.

Independent security/correctness and quality reviews found no actionable defect.
A fresh final textual pass also found none within its native/session diff scope;
that lane explicitly did not re-audit inherited authorization helper internals
or SQL timestamp defaults. Root checked those unchanged sources separately:
`lockProfileOwner` binds current subject/account/deployment,
`UserModuleAvailable` requires assigned registration/pass and enabled orders,
receipt creation uses `clock_timestamp()` and task creation defaults to `now()`.
This distinction is retained in the review record.

The test containers had a 2GiB limit without swap, 2CPU, Go memory limit 384MiB
and two test cases in parallel. Both exited zero without OOM. The exact two QA
pairs and dedicated PostgreSQL volumes were removed. All ten main service IDs,
images, start times and restart counts remained unchanged. No active test/build
process is left by these gates.

At this native store component gate, the main stand ran schema39/manifest `mvp-39`, image
`sha256:e6c429ba15abb5cc5782ddb066581cda6456a41f70be96e8e37db714aedd2170`.
The candidate is schema40/`mvp-40`; later owned activation is documented in
`shuttle-orders-submission-real-qa-2026-10-17.md`. The Git index is
empty; no commit, push, PR, publication or production deployment was performed.

The native HTTP routes and their separate component verification are now
recorded in `shuttle-miniapp-order-submission-http.md`. RU/EN form behavior is
recorded in `shuttle-miniapp-order-submission-ui.md`. Next: reviewed owned-stand activation
and all actual Telegram paths. Full proof/capacity/payment/meal lifecycle and
production acceptance remain separate required work.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
