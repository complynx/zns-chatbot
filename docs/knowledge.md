# Knowledge and private memo domain

`internal/knowledge.Service` stores general knowledge, event facts, suggestions
and a small owner-private memo. It adds no model provider or vector database.
The host must treat every stored text as **untrusted evidence**, including
reviewed facts. Text cannot grant identity, permissions, tool access or policy.
Agent tools, classifier invocation and user/reviewer interfaces are separate
integration work; the domain and authenticated routes are available here.

## Event metadata and retrieval

Migration 026 introduces `core.events` as a read-only view of
`core.pass_events(id, finishes_at)`. The existing pass-event timestamp remains
the sole authority. There is no second copied end timestamp. An order's sales
deadline is not an event end and is never used for this classification.

This view is a transitional common metadata adapter. Knowledge event IDs must
currently be registered in `core.pass_events`; knowledge scope rows reference
that table. A future physical common events table must move the booking and
knowledge references together and remove the old independent end column.
Do not create two writable authoritative end times. Order-only events require
an explicit metadata registration/migration; the module does not guess an end.

An event becomes `past` in each retrieval query when its end is less than or
equal to that statement's current database time. No cron, cache expiration or
copying step delays the transition. Before the end, the phase is
`active_or_upcoming`: the current metadata has no start timestamp, so it cannot
distinguish an underway event from a future one. General facts have phase
`general`.

Facts are keyed by `(scope, topic, fact_key)`. Empty event means general scope.
For a query with an explicit event, each topic/key resolves in this order:

1. Active fact for that exact event.
2. Active fact from the most recently ended past event, no later than the
   requested event's end.
3. Active general fact.

An exact event fact overrides historical text even if only the historical text
matches the search phrase: precedence is applied **before** text search.
Historical results retain their actual source `event`, `phase: past` and
`historical_fallback: true`. They must never be presented as confirmed facts
about the requested upcoming event. A query without an event returns general
knowledge only. Search is a bounded literal case-insensitive substring; `%`,
`_` and backslash are escaped. Topic/key ordering makes the first page stable.
Results are limited to 20 facts, each at most 2000 Unicode characters.
`untrusted: true` explicitly marks retrieved facts.

Removing a fact creates an inactive versioned record. It no longer participates
in retrieval; a historical/general fallback can then appear. An explicit
negative or corrected current-event fact should be curated as text when it must
override older guidance.

## Permissions, suggestions and review

`core.knowledge_permissions` contains explicit `(scope, actor, permission)`
grants. Permissions are `curate` and `review`. General scope does not grant
event scope and one event grant does not grant another event. Payment admins,
booking admins, names in messages and claimed ambassador status confer no
knowledge permission. The module grants no roles by itself; grants in this
stage are test fixtures only. Trusted administrative provisioning must grant
the appropriate scope explicitly.

- A curator can create, replace or remove a fact using its expected version.
- Any existing authenticated user can create a suggestion, limited to ten
  unresolved suggestions across all scopes. It starts as `pending_filter` and
  is not retrieved as a fact or exposed in the human review queue.
- The trusted host classification workflow calls `Assess(ctx, actor,
  Assessment)` for that actor's own proposal and expected version. A worthwhile
  result changes it to `awaiting_submission`, an author-private draft;
  otherwise it becomes `filtered`. Neither state exposes it to reviewers.
  This method has **no public API route and no conversational agent tool**.
  Classifier results cannot publish a fact, assign a role or select a different
  owner. The integration must generate the verdict from the classifier, not
  accept a user's asserted verdict. This does not require an elevated agent
  identity.
- The author must explicitly submit the displayed draft through the manual
  submission action before it becomes `pending_review`. Consent binds the
  proposal ID, version, event, topic, fact key and exact body. Another actor,
  changed draft or stale version cannot supply that consent. The submission
  rechecks current source authority, including on replay. It exposes only the
  consented body to permitted reviewers, not private source text or the
  classifier reason. An agent tool cannot submit consent for the author.
- A human with explicit `review` permission for the proposal's scope can approve
  or reject a submitted `pending_review` proposal. A reviewer cannot review their
  own suggestion, even if they also have curation rights. Curators may instead make
  an explicit direct edit under their separate permission.
- Approval checks the proposal version and the fact version captured at
  suggestion time. A concurrent curator correction cannot be overwritten by
  an older queued suggestion. Rejection can close such a stale proposal.

Users can list their own proposal statuses. Reviewers can list only another
owner's submitted `pending_review` proposals within an authorized scope. List
pages contain at most 20 records, newest first; pass the last ID as `after` for the next page.
Pending, filtered and rejected text never enters approved knowledge retrieval.

## Transactions, idempotency and privacy

Commands have an actor-scoped idempotency key, maximum 200 characters. Actor is
supplied by authenticated host context, never from command JSON. A transaction
locks the actor, then the knowledge scope where needed. Scope serialization
prevents competing approvals or curator edits from losing updates. Permission
rows are locked for the operation and checked before replay, so revocation also
blocks a previously authorized replay.

Fact/proposal/memo mutation, stored exact result and metadata audit commit
together. Reusing a key with a different request returns `idempotency_conflict`.
An exact retry returns the original result without a new mutation or audit row.
That result is a historical operation receipt, not a refreshed snapshot: query
current facts/proposal status afterward when current state matters. Optimistic
version conflicts return `knowledge_stale`; clients must refresh before a new
decision. Failed operations create no replay marker.

The private memo contains at most eight active named items, each at most 512
Unicode characters. Get/set/delete always address the authenticated owner's
namespace. Knowledge curators and reviewers do not gain access to another
user's memo. Keys contain only ASCII letters, digits, dot, underscore and dash,
up to 100 bytes. Deletion clears the current body and leaves a versioned inactive
record so concurrent edits and retries remain well-defined. A keyed GET returns
that version for a later replacement; an unknown own key has version zero.

Audit rows contain action/subject/version metadata, not text bodies. Exact
idempotency receipts remain actor-scoped database records. Deletion scrubs earlier
memory bodies from matching receipts while preserving hashes and version metadata;
replay returns an explicit redacted receipt without recreating the record.
Historical conversation archives and backups retain their separate policy.
API errors and server logs do not include raw
database diagnostics that could contain private text. All responses use
`Cache-Control: no-store`.

## Authenticated API

The root API registers `knowledgeRoutes` inside its existing authenticated
business mux. `requestOwner` supplies actor identity. Unknown JSON fields are
rejected; adding `owner` or `actor` cannot choose a different principal.

| Route | Purpose |
| --- | --- |
| `GET /v1/knowledge?event=&topic=&q=` | General or event-resolved approved facts |
| `GET /v1/knowledge/scopes` | Up to 20 event/general scopes with current explicit curate/review capabilities |
| `GET /v1/knowledge/fact?event=&topic=&key=` | Exact fact and version, including inactive records; no historical fallback |
| `GET /v1/knowledge/proposals?event=&after=` | Own proposal status page |
| `GET /v1/knowledge/proposals?event=&review=true&after=` | Explicitly permitted human review queue |
| `POST /v1/knowledge/actions` | Typed domain command |
| `GET /v1/me/memos` | Own active memo, maximum eight items |
| `GET /v1/me/memos/{key}` | Own named memo and version, including inactive records |

Public command names are `curate`, `remove_fact`, `suggest`, `review`,
`memo_set`, `memo_delete`. Public `assess` is forbidden. Relevant fields are
`key`, `event`, `topic`, `fact_key`, `text`, `version`, `proposal_id`, `decision`.
`review` decisions are `approve` or `reject`; `text` is the bounded review reason.
Suggestion creation uses version zero and snapshots the current fact version.
Memo commands use empty event/topic and their item key in `fact_key`.

```json
{"name":"suggest","key":"telegram-update-123-suggestion","event":"festival-2027","topic":"travel","fact_key":"venue","text":"Proposed event fact","version":0}
```

No embedding service, vector index, API credentials or new Go dependency is
required. PostgreSQL integration tests cover phase changes, retrieval precedence,
literal search, private ownership, capacity, scoped review, classifier separation,
self-review denial, stale edits, exact replays, permission revocation, concurrent
approval and actor/filter injection at the API boundary. Independent QA acceptance
remains a separate gate.

## Telegram and model integration

`/knowledge` opens current facts, event selection, own suggestion status and a
review inbox when the actor has explicit scope permission. `/memo` opens only the
actor's private notes. Natural language requests use the selected `knowledge`
skill; no text keywords route business intent. Fact creation/editing and memo
creation/editing are conversational. Manual cards support author submission of
private drafts, note deletion, scoped proposal review and retrying an unavailable
automatic assessment. Lists are bounded; proposal pages have an opaque next
button. Fact results expose at most
20 entries, and the agent can narrow them by topic or literal text search.

The seventh plan field, `knowledge_action`, is one of bounded reads or a typed
proposal. It contains no actor, version, permission or idempotency key. Initial
context contains capabilities and the actor's eight small memos. At most two
knowledge reads per Telegram update load approved facts, proposal status/review
queue or one exact memo. The host reserves each read in PostgreSQL before I/O.
A cancelled/interrupted read consumes its slot; a retry does not reset the
budget. Each result is bounded to 12 KiB; total knowledge context is bounded to
32 KiB, with explicit omission flags. Read failures do not become empty evidence.
Video refinement and knowledge reads share the replanning loop while retaining
their separate bounded budgets and original spoken-request context.

The host binds edits to versions actually returned in context, then caches the
typed command for retry. Missing active memo entries are not assumed to have
version zero: an exact memo read includes deleted versions. Exact fact reads
similarly distinguish missing/inactive facts from historical fallback. The
domain rechecks current permission before replaying an idempotency result.

After `suggest`, a separate bounded classifier request returns only a boolean
and a short reason. OpenAI and local Codex implement this narrow interface; the
remote model endpoint transports that verdict without an application actor or
API credential. The host calls owner-bound `Service.Assess`. A positive verdict
prepares an `awaiting_submission` private draft; explicit author consent is
required to enter the human review queue, and authorized approval is required
to publish. If classification is unavailable,
the proposal stays visibly `pending_filter` and its owner can retry. The saved
verdict prevents successful update replay from repeating the model call.

Author-submission callbacks bind the exact displayed draft and its expected
version. Approve/reject and note-delete callbacks reference stored owner-bound
commands through opaque tokens. The host supplies expected versions and stable keys.
Current domain authorization applies on every callback, including replays.
Review cards are refreshed from the current permitted queue; old/off-page or
revoked cards lose their action buttons. An agent `review_card` request only
opens the human review surface. It cannot approve a proposal.

Knowledge read payloads, private execution commands, classifier verdicts and
knowledge replies are excluded from generic conversation history. They remain
in their owner-scoped durable records for retry. Memory deletion scrubs matching
API receipt bodies and invalidates retained script output through deletion
generations. Historical Telegram archives remain separate. Facts, proposal bodies
and private memos remain untrusted model evidence, never authority.

Fact pagination: GET /v1/knowledge/page returns facts (at most 20), more and next_cursor. Continue with the same event/topic/text and cursor. Resolution selects event overrides before filtering/pagination; historical fallbacks retain source event and phase. Agent queries use a literal substring, not a list of keywords, and omitted pages are explicitly incomplete. The existing list endpoint remains compatible.

