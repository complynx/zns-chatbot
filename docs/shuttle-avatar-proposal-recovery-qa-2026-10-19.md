# Avatar administrator proposal recovery — 2026-10-19

Schema57 / manifest mvp66 source adds actor-owned read-only proposal recovery.
The installed primary remains schema49/mvp56; its images/configuration and domain
state were preserved. This stage adds no migration, dependency or service.

## Accepted checks

- 15 root integration tests pass against real isolated PostgreSQL, including four
 new proposal recovery/authorization/pagination tests and11 existing resolution
 tests. These prove original proposal recovery by either selector, completed
 immutable result recovery, expiry, current actor/revocation checks, byte-bounded
 cursor pages, HTTP authorization and unchanged receipt-only status behavior.
- 19 API root tests pass in the pinned Linux Go1.27.2 SDK container.
- Scoped golangci-lint reports0 issues. Regenerating sqlc leaves generated output
 unchanged. Two fresh independent textual-diff reviews report no actionable
 findings; the final review includes the current contract documentation.
- The PostgreSQL dump and logs are archived before exact-owned container/volume
 removal. The SDK container is also removed. Neither temporary container OOMed.
 The primary's11-container epoch and domain preservation checks pass.
- Index content remains unchanged. No commit, push or production deployment.

## Rejected first run

The first PostgreSQL run has13 passing and two failing root tests and remains
archived as rejected evidence. One failure compared the internal time location of
an otherwise identical instant: the new read now normalizes the authoritative
expiry to UTC, matching preparation. The other sent an unrelated proposal_id to
the pending-list endpoint and correctly received schema validation422 before
handler authorization. The denial probe now uses valid per-endpoint arguments.
The corrected fresh-database run passes all15 roots; failures were not relabelled.

## Acceptance boundary

These are store/HTTP behavior checks using real PostgreSQL and mock IAM/Telegram
peers, plus Linux API tests and source review. This stage does not claim a new
real Loom/Telegram/browser acceptance run. Previously accepted real IAM/Loom
repair runtime remains documented separately. Native administrator repair UI,
canonical proposal adoption between native and Loom operation IDs, and actual
bidirectional handoff acceptance are next. Reading a proposal is never consent.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
