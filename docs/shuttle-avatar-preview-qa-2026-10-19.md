# Saved avatar preview source QA — 2026-10-19

This bounded stage covers schema55 / manifest mvp63. It does not complete the
visual editor or draft finalization/send. No image was installed, no production
change or publication was made, and no real Telegram message was sent. The
primary remains schema49/mvp56.

## Behavioral proof

The isolated PostgreSQL command `pg-v2` passed all24 avatar integration roots:
four new preview roots and20 generation, transform, draft and rollback roots.
Preview cases cover native/agent descriptor and JPEG agreement, pinned public
frame bytes, foreign ownership, stale/invalid versions, unavailable frame,
concurrent edit and session expiry during processing, plus mismatched worker
source/transform provenance. They assert no new artifact, task, operation receipt
or outbox entry. The edit during rendering completes before rendering resumes;
image processing holds no database lock that blocks that edit.

This uses real isolated PostgreSQL and HTTP handlers with mock IAM/Telegram/Loom
peers. It is not browser QA or a real-agent conversation. Preview does not need
an active Loom run; runtime native-only finalization/send is still outstanding.

Linux `linux-unit-v2` passed59 unit roots across avatar, renderer, database, API
and task packages. Its25 no-DSN database roots skipped and are not counted as
PostgreSQL proof. Windows renderer/avatar checks passed10 roots with four POSIX
subprocess skips. The actual Linux CLI proof `cli-v2` built and invoked the child
executable through the parent protocol: explicit identity equals legacy bytes,
edited output matches its previous digest and invalid transform exits2.
The legacy JPEG SHA256 remains
`31a754361b05145895831fdd0e57e801a8442c7caff8127d116f3c59ea077728`.

The embedded PNG moved without byte changes into a codec-free package shared by
both binaries. A before/after bot dependency inventory adds only that package;
image codecs remain in the isolated leaf.

## Reviews and retained attempts

Independent correctness and quality reviewers checked the textual stage diff.
Fresh final reviews cover the schema-helper extraction. Reviewers ran no tests
or commands; execution evidence is separate. Final statuses and exact source
hashes are recorded in the ignored source gate.

The first frame-extraction unit attempt failed because a test still opened the
old loose-asset location. The fixture now reads the actual embedded asset while
retaining independent frame-hash and legacy-JPEG assertions; the later run
passed. Initial lint reported14 issues; the next attempt left one function-length
issue. These failed attempts remain retained. The final simplification inlines
one local method variable without changing the returned manifest. The follow-up
`pg-v3` command targets the four preview roots and two relevant manifest-contract
roots; its result and lint are recorded separately, not as a replacement24-root
command. No other product or test code changed after `pg-v2`/Linux/CLI proofs.

## Evidence and scope

Ignored evidence: `.cache/shuttle-work/avatar-preview-v1-qa/`, including
`pg-v2/`, `pg-v3/`, Linux/CLI checkpoints, Windows JSONL, lint attempts, dependency
inventories, retained originals and review/source-gate records. PostgreSQL dumps
and logs are archived before removing owned QA resources. Primary preservation,
resource removal, OOM state and final staged-diff checks belong to the gate.

The visual native editor, finalization and task-free durable delivery remain
next. Full native-only, agent-only and both-handoff runtime/browser acceptance
must precede a claim of completed editor parity. See the
[preview contract](shuttle-avatar-preview.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
