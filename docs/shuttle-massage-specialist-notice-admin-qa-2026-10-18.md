# Massage specialist notification administration QA — 2026-10-18

The new administrative diagnostics and exact retry/dismiss protocol pass thirty
selected race scenarios: twenty-eight integration roots and two observability
roots, with sixty-seven passing test events and zero failures or skips. This is
source/mock HTTP acceptance. Main installation, real Telegram and process
restart acceptance for this feature remain open; the main stand still runs
schema48 / mvp-55.

The preceding [worker gate](shuttle-massage-specialist-notice-worker-qa-2026-10-18.md)
accepts mock delivery and in-process service lifecycle behavior. This stage adds
`massage_specialist_notifications` and `massage_specialist_notification` to the
administrative agent API, and `massage_specialist` to `resolve_notification`.
Current global administrator authority is required at the HTTP boundary.

## Accepted behavior

List responses have bounded twenty-row pages, UUID cursors and state filtering.
List and detail are deployment-scoped. Dedicated DTOs exclude client name,
profile, contact and outbound Telegram payload. Detail adds the attempt ID and
Telegram receipt; the worker receipt contains message/chat IDs. Tests cover
pagination, filtering, foreign deployment reads, non-administrator denial and
absence of client data. The detail API also keeps the existing response-size
bound.

Resolution uses the existing exact proposal, operation ID, reason, expiry and
immutable receipt protocol. Unknown retry warns that Telegram may already have
received the message and a duplicate is possible; it remains a last resort.
The proposal has exactly one family FK. A small shared builder now selects that
FK for payment, pair, order, owner massage reminder or specialist notification.
The final gate includes successful retry/dismiss checks for all those families.

Specialist retry rechecks local account/module/binding/preference/booking
eligibility at commit and never rebases the captured binding or preference
version. Worker reservation repeats its checks after IAM/network waits. A
mute/re-enable or revoke/regrant after preparation cannot revive an old intent.
Module disablement, recipient revocation and booking cancellation also reject
a stale creation retry. Dismiss remains available to the current administrator
after those target-side changes.

Commit conflicts follow the existing protocol: HTTP 200 with `status: rejected`,
`error.code: notification_resolution_rejected` and terminal classification. They
do not create a successful resolution receipt. Preparation has its separate
HTTP error mapping. Tests distinguish these paths and also cover changed exact
parameters, expired proposals, wrong event and wrong notification family.

A successful operation retains its original resolution receipt through proposal
expiry and later delivery. Status/reconcile and a repeated commit return that
receipt without resending. Tests compare the booking before/after, preserve the
captured generations and assert no new payment reviews; they do not establish a
full business-table fingerprint gate. HTTP peers and synthetic test identities
are used. No production IAM, real Telegram or Loom developer stand was changed.

## Review and retained failure

Fresh final correctness review and a separate quality/runner review returned no
actionable findings. One earlier review found incorrect HTTP-409 expectations in
new tests; they were fixed to assert the existing rejection envelope before
execution. Initial lint findings led to smaller FK/DTO construction and ordinary
syntax/style corrections. Final scoped Linux lint for `internal/db`,
`internal/api` and `integration` reports zero issues. Reviewers used textual diffs
and source reads only, without commands or tests. The index remains empty.

The first runtime attempt failed because the new fixture tried to reassign the
existing `alex` alias to another identity. Binding identities are intentionally
immutable. Its logs and failure summary remain in the v1 evidence directory.
The corrected fixture assigns the previously unbound `beth` alias to the test
recipient, keeping the administrator distinct. Production binding policy and
resolution behavior were not weakened. A fresh final review covered the
corrected fixture, followed by the successful v2 run.

Both attempts use separately named, owner-labelled PostgreSQL/Go containers and
private database volumes. Limits are 512 MiB and 2 GiB, respectively. No OOM
occurred. Exact temporary resources are removed after terminal results; shared
Go caches remain. The successful gate confirms its source freeze and ten main
Compose container identities, images, start times, running states and restart
counts. It does not claim a live business-row fingerprint or deployment gate.

Ignored evidence: `.cache/shuttle-work/massage-specialist-notice-admin-v2-qa/`
contains the accepted checkpoint, source freeze, expected roots and race logs.
The v1 directory retains failed runtime evidence and final corrected lint output.

Next: shared agent/native global specialist preference endpoints and controls,
then complete feature-wide mock QA, manifest revision and reviewed migration /
recovery preparation before main installation and real Telegram acceptance.
Upcoming-session notices remain a separate later milestone.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
