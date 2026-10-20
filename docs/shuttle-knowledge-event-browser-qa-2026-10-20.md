# Event knowledge native browser QA, 2026-10-20

Status: the bounded desktop browser workflow passes on an isolated owned stand.
This supplements the [event API/agent gate](shuttle-knowledge-event-runtime-qa-2026-10-20.md)
and [general knowledge browser gate](shuttle-knowledge-browser-runtime-qa-2026-10-20.md).

The stand uses the pinned mvp83 image
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`,
real PostgreSQL and Zitadel, and actual deployed Mini App HTML/JS in headless
Microsoft Edge. Only the Telegram SDK and transport are fixtures. Loom is stopped
gracefully before the browser workflows begin and remains stopped throughout;
its run count remains zero.

## Accepted path

Two active synthetic events have separate administrators. Capability reads confirm
that event A's administrator can edit A but not B, event B's administrator cannot
edit A, and the general knowledge moderator cannot edit current event A.

Five fresh browser contexts exercise three distinct users:

1. An ordinary participant selects event A, prepares a new article suggestion,
   reloads the page and confirms the same operation, proposal, event and terms.
   The proposal remains pending until a different authorized reviewer acts.
2. Event B's administrator selects event A and has no direct-edit, pending-review
   or decision controls. This context sends no knowledge write.
3. Event A's administrator discovers the original pending proposal, reviews its
   complete body and explicitly approves it. The recorded proposer and reviewer
   are distinct; the published article retains event A.
4. The same event administrator edits that article against revision1 and explicitly
   confirms revision2. The article ID and event remain unchanged.
5. The participant reads both the original published revision and the current
   revision. Direct editing remains unavailable and this context sends no write.

The reload proof concerns the original prepared operation. The browse collection
resets to `all`; this is recorded explicitly and is not filter-restoration
acceptance. Confirmation still uses the original event-scoped arguments.

HTTP results and final database counts agree: one article, two revisions and three
`suggest`/`review`/`edit` operations. Separate administrative fixture setup is not
included in that count. All five browser contexts log out and close without page
errors. Durable before-dispatch journals retain the original write identities.

## Review, cleanup and limits

Fresh textual review found an incomplete-provisioning guard in the QA helper;
that guard was fixed before fixture/browser execution. The recovery proof was
also narrowed to distinguish operation recovery from browse-filter persistence.
The final fresh review is clean. No product code changes were needed for this gate.

All nine exact owned services, volumes and network are removed. Shuttle, Loom
and Zitadel database archives are fully decoded before deletion. Decoding is not
a restore test. The primary stand remains mvp81/schema65 with its original
companions and durable state preserved. Local retained evidence is under
`.cache/shuttle-work/knowledge-event-browser-runtime-v1/` in the primary checkout,
including `readback.json`, browser results/journals and `cleanup-proof.json`.

This accepts the named native event workflow on desktop Edge. It does not accept
mobile/real Telegram WebView, arbitrary model comprehension, exact native-to-Loom
prepared-operation adoption, separate physical devices, fault injection, primary
installation or production readiness. Event archival and revoked-rights evidence
retain their separate previously documented boundaries. Full remote CI is still
running at this checkpoint; no CI success is inferred from this QA result.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
