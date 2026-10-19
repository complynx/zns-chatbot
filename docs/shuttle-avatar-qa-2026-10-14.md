# Avatar acceptance — 2026-10-14

This is a bounded framed-JPEG slice, not completion of all media work. The module
does not change Telegram profile photos, classify uploads from menu state, or
store original source bytes. See [the contract](shuttle-avatars.md).

## Developer checks

The architect and Go implementation discussion chose a leaf rendering executable
and durable owner-scoped PostgreSQL artifacts. The frame is byte-identical to
the existing bot's frame. JPEG/PNG decoding, EXIF orientation, center-square crop,
white flattening, frame compositing and JPEG output have focused checks.

Behavioral HTTP/PostgreSQL tests cover generation, owner reads, download hash,
operation replay after restart/source loss, shared text/document outbox ordering,
foreign owner/admin denial, revocation, invalid run/source, renderer failure and
uncertain Telegram delivery without automatic replay. A fast Loom call before
the bot persists its run binding receives a temporary failure before any source
read or render; retry with the same operation succeeds after binding.

Developer checks found and fixed two concrete problems before native acceptance:
an embedded buffer exposed a promoted `ReadFrom` method that bypassed the child
output cap; and the fast run-binding path could reject a valid operation before
the create response was persisted. Each has a failing-before/passing-after test.
Fresh independent correctness and quality reviews of the final avatar source
reported no actionable findings.

The first full race run failed the profile neutral-follow-up test. A controlled
lost Telegram response reproduced its missing language excerpt. Successful agent
results now remain language evidence when their delivery is gated. The repair
does not retry messages. Three focused runs passed; fresh correctness and quality
reviewers found no actionable issues. Both failed runs remain in ignored logs.

The final complete Linux race suite passed: integration 186.171 seconds, with
explicit `./cmd/... ./internal/... ./migrations/... ./integration/...` package
scope. Final lint reported zero issues. Pinned `govulncheck` v1.8.0 found no reachable
vulnerabilities for either Linux executable; this is a dependency scan, not a
security certification. License/version/provenance evidence is retained in
[the dependency audit](dependency-audit/2026-10-14-avatars/README.md).

## Native acceptance

The owned stand runs schema 20, manifest `mvp-16` and image
`sha256:8e698c3941855eac77a0cfaf4ad4f56d9dc0bf67aed203949986a0420b8c40da`
tagged `shuttle-test-bot:avatar-candidate`. Use its ignored candidate override
together with the Loom fixture and resource overrides. The prior image is retained
as `shuttle-test-bot:pre-avatar-20261014`.

The pre-migration PostgreSQL archive was validated with `pg_restore --list`:
`pre-avatar-20261014.dump`, SHA-256
`ca1ac47c899657fa91b305fd75d7fb18d9999be49799d0c6ec70f9fa6b517d7e`.
Migration ran separately before the candidate started. Existing user/task/profile,
registration/pass/evidence/review/pair digests were unchanged by migration.
The first rollout preflight used an `id` ordering for the profile table, whose key
is `user_id`. It stopped before changing the application or database. The original
archive was kept; corrected deterministic ordering passed.

The authorized Telegram account uploaded one generated geometric PNG without
real biometric data. Native Loom called `zinusya_avatars.generate`; the separate
executable created and persisted the JPEG. The first send received a definite
403 from our test egress guard, which lacked multipart support. It never reached
Telegram. That failed task, artifact and receipt remain intact. Local closure
correctly refused the known terminal failure; no database state was rewritten.

The guard now validates bounded multipart uploads against the same sole recipient,
canonical MIME headers and exact file fields. Independent review found a multipart
parser ambiguity and duplicate JSON-key ambiguity; strict parsing now rejects both.
All 10 test-infrastructure behavior tests pass. The final fresh guard reviewer
reported no actionable findings.

A new explicit request in the same authorized chat generated another artifact from
the original source reference. It delivered `zinusya-avatar.jpg` and the final text
reply. The 1024-square JPEG is 584566 bytes with SHA-256
`ac1b38ba5fcf2736fdefaddee15571a90be720f995cfca8ede6701452d66d58d`.
The downloaded result and Telegram screenshot were saved and visually inspected.
The original caption/source and generated document are separate messages.

An actual bot restart preserved the bytes and original receipt. Replaying the same
generation operation after its run completed returned the original result, with
one document intent and no second delivery. Native owner `operation`, `get`,
`fetch` and authenticated content reads passed; a different global administrator
could not read the owner's artifact or receipt. Registration/payment/pair/profile
digests stayed unchanged throughout avatar QA. Both document and final reply had
delivered outbox receipts. There were zero nonterminal owned Loom runs.

Detailed proof is kept in ignored `native-avatar-checkpoint.json`. The completed
race-test containers and their disposable PostgreSQL container/anonymous volume
were removed. The necessary bounded live-test services remain.

## Boundaries

The real-IAM/Loom stand uses a manually driven Codex provider mailbox. It can
prove protocol, authorization, executable rendering, persistence and delivery;
it cannot establish unattended model classification or response-language quality.
The current ceiling is 20 artifacts per owner, not a production retention policy.
No face processing, manual crop editor, alternate/event frames, avatar import or
original-byte storage is accepted here. Interactive browser CA/zitadeltg linking
remains deferred to the agreed user session. No production release is claimed.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
