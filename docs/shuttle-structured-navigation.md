Current source also adds [native start and avatar entry](shuttle-native-start-avatar.md).
Its source acceptance and installation boundary are recorded separately.

# Structured Telegram navigation

This document records the initial navigation slice. Later work adds native
[registration and required details](shuttle-registration-details-native-entry.md),
[conversational questions](shuttle-agent-reply-keyboards.md) and the
[orders draft form entry](shuttle-orders-selection.md#native-entry). The /orders
command is installed on the owned stand at schema38; see its
[runtime acceptance and limitations](shuttle-orders-selection-qa-2026-10-16.md).

`/menu`, `/profile` and `/registrations` are durable owner read/navigation commands.
They use the existing intake, authorization, queue and outbox without creating an
agent session or calling Loom. A pending agent question remains available after
navigation. This is the first foundation for the required complete structured,
agent and mixed workflows; it does not yet implement registration/name writes,
Mini App forms, pair actions or reply keyboards for agent questions/approvals.

## Routing and recovery

Normalization recognizes only these exact commands, with an optional canonical
UUID page cursor for `/registrations`. Attachments and unsupported media cannot
be reclassified as commands. Ordinary free text retains its agent routing; a name
expectation does not consume it as a structured name answer.

A structured task can be routed ahead of earlier unrouted messages. The short
conversation lock still protects routing, and its active task ID stays unchanged.
The routing watermark advances only through a contiguous prefix of routed tasks.
When an earlier ordinary message is routed later, it advances across the already
routed navigation tasks; ordinary messages retain their relative routing order.
No additional workflow engine, worker, database table or migration was introduced.
The additional query is kept in the sqlc source and generated code.

Reads bootstrap the existing canonical IAM account if needed, obtain the normal
delegated user credential and validate subject/organization before owner storage
reads. The identity client validates the exchange actor. IAM failures keep the
accepted task retryable; local revoke denies access. Outbox delivery performs its
existing current-authorization checks. Loom availability is not a prerequisite.

The response and completed task checkpoint commit together, with per-task outbox
keys. Restart reuses the stored response and its concrete keyboard type. The
existing uncertain-send policy applies: a lost Telegram send receipt does not
permit blind retry or establish exactly-once delivery.

## Views and keyboards

The menu, profile and final registration page use `ReplyKeyboardMarkup`.
Read-only pagination uses a localized inline Next page button with a compact
owner-scoped cursor. It grants no authority and is never an approval. Legacy typed
page-cursor commands remain readable for already sent keyboards. The Go sendMessage model admits only typed
inline, reply, removal and ForceReply variants; decoding a stored payload restores the
concrete variant. Old inline outbox payloads remain supported by the new code.
Agent questions and approvals still use their earlier inline presentation until
the separate durable question/operation binding and reply-keyboard stage is done.

Profile reads the saved name and pending expectation without changing either.
Registration status uses the same owner account read as Mini App/agent tools,
with bounded pagination, event-local times and module availability. Pass amounts
are confirmed ledger values; unreviewed payment evidence is not confirmed money.
The initial deposit amount/deadline is separate from total remaining and final
payment deadline. Disabled registration modules suppress pass details. User/event
names are plain text, never interpreted as Markdown. Interface text uses the
existing saved-language and Telegram-language fallback rules.

The next actual user-triggered agent create/control request includes up to five
prior completed navigation request facts from the same owner/conversation. Only
command and acceptance time were included in the accepted navigation image, as
untrusted history that proves neither
consent nor delivery. Existing create/control checkpointing freezes this context
for an uncertain retry. Recording navigation starts no proactive agent turn.
The newer working tree also includes completed Telegram profile operations with
a matching immutable receipt and profile_version; see
[direct Telegram name entry](shuttle-telegram-name-entry.md). These changes are
not yet in the deployed navigation image. Mini App write-action facts still need
their own transactional context integration.

## Historical navigation-image component and owned native acceptance

The accepted pagination source, before subsequent profile/name work, passed fresh Linux race runs of all task and Telegram
unit packages plus14 selected real-PG integration roots:41 root tests /92
root-and-subtest pass events, with no skips or failures. These final receipts
rerun both unit packages; they do not reuse the earlier navigation receipts.
They are not a full integration-suite rerun. Strict lint reports zero issues.
Existing question/cancel, profile-context, account-status and dual-upstream
restart paths are included. A fresh independent pagination source review found
no actionable findings.

New behavior checks exercise zero Loom calls, duplicate updates, routing ahead
without skipping ordinary-message gaps, a pending agent question through Loom
outage/menu use/bot restart, unchanged name expectation, manual history on the
next genuine agent input, saved reply/inline/removal payloads, language/plain
text, revoked-owner denial and a real owner pass/deposit/balance status read.

Two initial source reviewers found an actor/issuer comparison bug and misleading
deposit deadline display; both were verified and fixed, and a fresh source review
found no actionable findings. Failed evidence is retained: the actor bug; a new
status test using Event.Name instead of Event.Document.Name; and a denial-locale
assertion requiring Russian after revocation, although the existing access-denied
path correctly fell back to the update's Telegram language, English. All failed
runners exited1/OOMfalse. The accepted runner and isolated PostgreSQL exited0
without OOM and were removed by verified identity. The main ten stand services
retained their fingerprints during these component checks. Private evidence is
in ignored `.cache/shuttle-work/structured-entry-qa/`.

Owned native Telegram acceptance passed on the final image
`sha256:7521d9a0f305aa3e335c9a0b983870e8c6f86381ed8911a3514005bd3ffae87c`.
The image-only cutover kept schema33, all64 table hashes, configuration and
companion instances unchanged. A real-IAM owner profile read also passed before
the same poller resumed a fresh successful polling cycle.

With the actual owned Loom stopped, the current approved Telegram account used
/menu, /profile, /registrations and the localized Next page button. All four
structured tasks completed and their outbox messages were delivered while Loom
was still stopped. Two pages showed ten existing fixture registrations. Eight
business-table hashes and Loom run/input counts stayed unchanged; the same Loom
container was restored and passed readiness. Native evidence and the screenshot
are in ignored `.cache/shuttle-live-test/structured-pagination-native-qa/`.
The first native pass exposed a technical UUID in the pagination keyboard; that
receipt is retained separately, and the readable-button fix received fresh source,
component, image and native checks.

The final image build exited0. Its bounded BuildKit cgroup recorded memory-limit
hits (max1763), with oom/oom_kill0. Intentional builder shutdown exited1 without
OOM; the builder/cache and terminal Go/PostgreSQL QA containers were removed by
verified ownership. Build and component receipts are retained under ignored
`.cache/shuttle-work/structured-pagination-image-qa/` and
`.cache/shuttle-work/structured-pagination-qa/`. No dependencies changed.

Complete structured registration, direct name entry, forms, agent questions via
reply keyboards and four-way mixed continuation remain required and unaccepted.
Real WebView trust/login is a separate deferred gate. This read/navigation
acceptance does not close those workflows or establish production readiness.

## Application rollback boundary

The new binary reads legacy inline keyboards. The previous binary cannot handle
new structured routes or restore reply-keyboard outbox payloads. Before returning
to the previous image, stop ingress and drain/reconcile every structured task and
its undelivered outbox; do not strand accepted work by treating old-image startup
as proof of reverse compatibility. Preserve durable records and uncertain-send
receipts. Schema stays33. No schema rollback, production deployment, commit or
push is part of this slice.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
