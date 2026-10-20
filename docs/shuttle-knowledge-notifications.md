# Durable knowledge notifications

Status: implemented in source mvp82 with additive migration66. The owned main
stand remains schema65/mvp81. This source has not been installed or accepted
against a full real-IAM/Loom/mock-Telegram stand.

## Recipient policy

Pending suggestions notify explicitly assigned general/history moderators or the
active event's assigned administrators. The proposer is excluded from review
notices. Ambassador status alone is insufficient. A global IAM role permits
administration but does not subscribe every global administrator to review
notices. Global administrators can still open authorized event review normally.

When an event ends, unresolved suggestions become eligible for general/history
moderators immediately, before physical archival. The existing irreversible
archive marker also preserves historical routing after rescheduling. A notice
prepared for an event administrator is blocked if that person no longer has the
appropriate current authority. New moderator assignments can discover older
pending suggestions on subsequent maintenance scans.

An approved or rejected suggestion notifies its proposer. Notification delivery
never changes publication or review state and never launches an agent run.

## Durability and delivery

The committed suggestion is the durable source. Maintenance inserts at most100
missing recipient records per pass. A unique key on deployment, suggestion,
recipient and kind makes repeated repair safe. Recipient discovery does not
copy titles, proposed text or review reasons into delivery state. Existing
retained suggestions can produce their missing notices when this feature starts.

The existing River queue schedules IDs and versions. Before reservation, the
worker verifies the recipient through IAM. The reservation transaction checks
current account, suggestion state, event lifecycle and local moderation rights
using publication-compatible lock ordering. A revoked or superseded recipient
is blocked. Delivery uses the existing shared Telegram outcome handler and its
bounded shutdown cleanup. Explicit HTTP429 rejections are retryable and keep the
same notice ID across restart. An interrupted reserved send or ambiguous reply
becomes `delivery_unknown`; it is not automatically resent. A previously blocked
or delivered recipient/kind pair is not recreated when authority changes again.

Plain EN/RU notices contain no proposed content. The Mini App link selects the
exact suggestion for a fresh authenticated read; it is neither consent nor a
write. An existing unfinished operation takes precedence over that link.

## Agent diagnostics and observability

`zinusya_knowledge.notifications` returns delivery locators and state, without
payloads, proposed text or credentials. Users see their own recipient records;
current global administrators may inspect deployment-wide metadata. Default
limit10, maximum20; pass `next` as `after` for the next page. Delivery state and
moderation state must be interpreted separately. The method is read-only.

The new worker is included in the existing bounded queue and worker-duration
metrics, and uses the `knowledge_notifications` activity component. No new
telemetry backend or configuration is required.

## Migration and acceptance boundary

Migration66 adds a notice table, indexes and a current-recipient view. It does
not rewrite articles, suggestions or operation receipts. Its down migration
refuses to discard any notice history; export and explicitly resolve rollback
before removing a nonempty table. Preceding-binary compatibility on schema66
still requires runtime proof.

The focused Mini App gate passes60 cases, including28 knowledge cases. Affected
DB/task/API package tests and observability tests pass; strict lint reports zero
issues. The initial unit run exposed two stale metrics-cardinality assertions;
the new known worker adds four duration series and three queue series. Those
expectations were corrected. Initial lint formatting/comment failures are also
retained under ignored `knowledge-notices-source-v1/`.

The frozen all-knowledge Linux/PostgreSQL race gate passes25 roots/42 terminal
cases with no failures, skips or OOM. It uses mock IAM/Loom/Telegram HTTP peers
and real PostgreSQL; it is not the separate full real-IAM/Loom/mock-Telegram
stand. Source correctness and quality reviews found no actionable findings. Full standalone mock-Telegram
acceptance, real Loom moderation dialogues, separate-device handoffs and exact
API tools for human-authorized resolution of ambiguous delivery remain open.
Do not install this candidate on a real Telegram stand to exercise synthetic
reviewer identities. Multi-user delivery belongs on isolated mock Telegram.

No production-readiness or production-deployment claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
