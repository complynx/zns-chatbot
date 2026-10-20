# Event knowledge runtime acceptance, 2026-10-20

The isolated `kb-event-r1` gate passes native-API suggestion → actual Loom
review by that event's administrator → free-text consent → one publication.
It also passes automatic historical archival and current moderator authority.
The nine-service stand is archived and removed; the primary stays mvp81/schema65.
This is controlled-provider integration evidence, not whole-feature acceptance.

The candidate is mvp83/schema67, image
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
It uses real isolated Zitadel and Loom, the counted synthetic provider and mock
Telegram. Production, real Telegram and the Loom developer's stands are untouched.

## Event authority and publication

Five synthetic users bootstrap through the Mini App HTTP API. A named general
moderator is assigned. Two synthetic events get distinct administrators through
the existing authenticated event and exact authority APIs.

Capabilities and actual edit preparations reject cross-event administrators.
The general moderator has no implicit editing authority for an active event.
An ordinary user submits one event-A suggestion through the native HTTP API.
It remains pending with no article or Loom run. Review preparations return 403
for the ordinary proposer, general moderator and event-B administrator. This
ordinary-proposer check does not separately prove privileged self-review denial.

One actual Loom run for event A's administrator reads the frozen proposal through
`script` and prepares `zinusya_knowledge.review` through `module_call`. Exact
terms and an ordinary reply keyboard arrive in mock Telegram. The prompt has no
approval ID or argument hash, and publication remains blocked. A plain Russian
consent is bound to the pending approval through `interaction_resolve`.

The result has one article, one revision and distinct proposer/reviewer identities.
The article retains event A as its origin. Native status and repeated native
confirmation return the identical review receipt. The original suggestion receipt
still says pending at this publication checkpoint; the current suggestion is
approved. The original bot task completes with one accepted final reply. There
is one Loom run and two bot tasks; this report does not assert that both tasks
are completed. No restart is performed in this gate; the separate
[general content gate](shuttle-knowledge-content-runtime-qa-2026-10-20.md)
covers pending-consent graceful restart.

## Automatic historical transition

The administrator changes event A's end to a past instant through the event API.
The fixture does not wait for natural event expiry or call an archive method.
The ordinary background maintenance worker creates exactly one archive revision,
revision 2. The body, event ID and `Europe/Minsk` provenance remain intact.

Published search returns the article in the general collection with its event
filter and returns no article in the active event collection. The previous event
administrator, event B's administrator and the unnamed global administrator all
receive 403 for a historical edit preparation. The named general moderator then
makes an exact native edit, producing revision 3.

Revision 1 still contains the original body. Current reads return the historical
correction with the same event origin. The former event administrator can still
read the identical original review receipt after these later revisions. There
is still one article and one Loom run: archival starts no proactive agent dialogue.

## Evidence and limits

Ignored evidence is retained under `knowledge-event-runtime-v1/`:

- `readback.json`: event-scoped publication and native replay.
- `archive-readback.json`: automatic archival, search transition and historical authority.
- `checkpoint.json`: retained request intents, operation receipts and final phase.
- `cleanup-proof.json`: three fully decoded PostgreSQL dumps, final Telegram
  state/journal, removed owned containers/networks/volumes and preserved primary.

Source/controller review closes in two passes. A retained-intent naming collision
was corrected before runtime; no failed runtime gate was relabelled. The original
failed general-content and cleanup-recovery evidence remains separate.

This gate uses native HTTP calls, not browser form editing or a separate-device
handoff. Model replies and tool choices are controlled; autonomous understanding,
retrieval quality and token/cost accounting are not accepted here. All content is
synthetic and has been removed with the disposable realm. Main installation still
requires the separately observed complete remote CI gate.
