# Shared and event knowledge

Status: architecture agreed; store and agent API are implemented in source.
Native interfaces are present; browser and whole-feature acceptance remain open; see the
[foundation evidence](shuttle-knowledge-foundation-2026-10-20.md).
This document defines target behavior; the foundation evidence tracks implemented
and accepted scope.
Shuttle is the rewrite codename; the service and agent identity remain zinusya.
This is communal published knowledge, separate from private imported user
conversation history in `zinusya_history`.

## Two collections, one storage service

Use PostgreSQL and the existing authenticated API/tool protocol. Do not add a
vector database, external search service or autonomous Loom pipeline.

- General: zouk, community reference material and historical event articles.
- Event: published material for a specific upcoming or ongoing event.

An article has a stable ID, slug, title, language, topics, Markdown body, source
links and a current revision. Event articles retain their event ID permanently.
Event provenance includes the event name, dates and timezone; historical reads
show that the material describes that event, rather than current conditions.
Articles are bounded documents. Published revisions are immutable snapshots with
actor, time, previous revision and, for suggestions, proposer and reviewer.
Archiving does not replace the original text or erase its sources.

No factual articles are fabricated as seeds. Empty collections are valid until
moderators supply or approve material. Private messages, receipts, passport
information and payment evidence are not automatically imported into knowledge.

## Agent discovery and reading

Expose a knowledge module in the zinusya manifest. Every returned article or
snippet is untrusted reference data, never instructions to the agent.

- `overview`: bounded available collections, events and topics, including short
  descriptions and counts visible to the caller. Help the agent choose filters
  without guessing opaque event IDs.
- `search`: one to three query formulations with optional collection, event ID, language and topic filters;
  return bounded ranked titles/snippets, article IDs, matching section anchors,
  revision, collection, event origin and source links. Search only published
  current revisions. Pending suggestions are never search results.
- `list`: browse titles/topics for a collection or event without inventing a
  search phrase. Pagination has an explicit cursor and a bounded page size.
- `read`: retrieve an article, optionally one heading section, at the discovered
  revision. Return its contents, heading index and provenance. A stale revision
  request must be explicit; do not silently substitute new content.
- `find`: literal case-insensitive search inside one exact article revision,
  returning bounded matching passages and their section/line references.
- `revisions`: inspect published revision metadata and explicitly read an older
  published revision. Historical results are labelled and are not current terms.

Start with PostgreSQL full-text search, title/slug matches and topic filtering.
Support Russian and English text without assuming the query language matches the
article language. Rank exact title/slug matches before body matches; pagination
has a stable article-ID tie break. An empty result is a valid result, not evidence
that the subject does not exist. The agent can broaden the query or browse topics.
Each response reports applied filters, visible result bounds and continuation;
unknown filters are errors, never silently ignored. Refine or broaden explicitly.
Multiple queries share one scope, deduplicate article/section matches and retain
which query matched. Reads use stable article/revision references across restarts.
No embedding/model call is required for indexing, search or read availability.

The [retrieval/tool research](shuttle-knowledge-tool-research-2026-10-20.md)
separates source observations, agent preferences and the chosen initial contract.

For an event-specific question, the agent searches that event first. It may
separately search general reference or history, but must identify historical
sources and must not present old prices, deadlines or rules as current event
policy. Search filters must not silently cross into a different event. Return
citation-ready article/revision/section identifiers so answers can be traced back
to the exact published text. Do not inject the entire corpus into every prompt.

## Editing and moderator review

Authenticated active users can read published articles and submit suggestions.
A suggestion freezes the exact replacement content, reason, target article and
base revision; new-article suggestions freeze their proposed collection/event.
The proposer receives a durable suggestion ID and can read its decision.

General/history editing and review require an explicit active knowledge moderator
assignment. Existing event administrators edit and review only their event's
active knowledge. Ambassador authority alone is insufficient. Global
administrators manage moderator assignments through exact-approved audited
operations; possession of the global role does not silently publish an ordinary
user's suggestion. Reuse current event-administrator/global authority policy for
event recovery and document any global-administrator action in its audit.

The agent has `suggest`, `my_suggestions`, `pending_suggestions`, `edit`, `review`
and moderator-assignment tools. Mutating methods use the existing exact
prepare/confirm/status/reconcile protocol, immutable arguments and durable
operation receipts. A proposer approving their own Loom prompt is not moderator
approval. A moderator independently discovers the suggestion, reviews its exact
content and confirms their own authorized operation. Privileged direct edits use
the same version and exact-consent boundaries.

Authenticate author/reviewer from the caller; never trust caller-supplied actor
IDs. Recheck account, current authority, article revision and event lifecycle in
the commit transaction. Concurrent edits conflict instead of silently merging.
Approval/rejection is recorded once; a changed retry cannot reinterpret a prior
operation. Rejection preserves its reason and does not publish the proposal.
Durable notices alert the appropriate reviewers; notification failure cannot
lose a suggestion. Pending-list discovery remains available after restart.
Only the proposer and currently authorized reviewers can read pending content.

## Native and mixed workflows

Provide `/knowledge` entry and Mini App collection/event/topic browsing, search,
article/section reading, suggestion submission and decision status. Authorized
moderators and event administrators have pending-review and exact edit/review
forms; global administrators can manage named moderator assignments. Ordinary
users can complete a suggestion and a different authorized reviewer can decide
it without Loom. All API and agent methods share the same article, revision,
suggestion and operation identities. Native forms can continue agent-created
suggestions/preparations with fresh exact consent, and the agent can discover
native-origin work without creating replacement business objects.

## Event completion

Use the event's stored absolute `ends_at`; display dates in its IANA timezone.
The existing bounded maintenance loop archives ended event knowledge independently
of Loom/IAM availability. Lock the current event before inspecting its end time
and applying the move. Record archival provenance atomically, preserve article
IDs, source event, revisions and outstanding suggestions. Repeated sweeps do not
create duplicate articles or revisions. Partial batches resume after restart.

At end time, `overview`, `search` and `list` use effective lifecycle state:
ended event articles immediately appear in general/history and leave active-event
discovery, even before the physical sweep. Explicit event-origin filters can
still find them as history. Reads classify the material as historical and writes require
current general/history moderator authority, even before the sweep catches up.
Old event-admin preparations cannot publish after the transition. Pending
suggestions remain available to general moderators and need fresh preparation
against the current revision/lifecycle. A later change to event dates does not
silently reopen an already archived knowledge collection.

The initial conversion is deterministic: historical classification plus event
provenance and original text. It does not automatically assert that a planned
activity happened. Any later summary or past-tense rewrite is a new proposed
revision with sources and moderator approval. No unsolicited reactive-agent run
is launched by archival.

## Implementation and acceptance

Reuse the existing event authority lock order, transaction receipts, API schemas,
manifest conventions and bounded maintenance cursor. Add migrations/sqlc queries
for articles, revisions, suggestions, moderator assignments and exact write
preparations. Preserve existing data and keep the preceding binary's read/write
paths independent of these new tables.

Required behavior gates:

1. Two published collections are discoverable through the real agent tools;
   search/read return bounded exact-version excerpts with source provenance.
2. Ordinary users cannot edit or inspect other users' pending suggestions;
   an authorized reviewer can publish or reject the exact proposal.
3. Revoked moderator and wrong-event admin fail at execution; an ambassador
   cannot acquire editor authority through payment privileges.
4. Native API and agent operations share one result across retry, lost response,
   restart and stale revision; proposer consent cannot replace reviewer consent.
5. Event-end sweep moves documents once, retains original text and event identity,
   survives restart, honors rescheduling before archival and rejects stale
   event-admin writes while allowing current historical moderation.
6. Delaying the physical archive sweep never delays historical discovery or
   exposes ended event articles as current. Human-only native and both mixed
   directions preserve the original suggestion/operation and distinct reviewer.
7. Search never leaks pending content or silently substitutes historical event
   conditions for current ones; Russian/English discovery and section reads work.
8. Independent correctness/security and quality reviews pass, then owned
   real-IAM/Loom/mock-Telegram QA exercises ordinary user plus distinct moderator
   and event administrators. Record failures and cleanup temporary containers.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
