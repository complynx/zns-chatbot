# Progressive shared and private memory foundation

This layer uses approved facts, legacy owner-private memos and private topic documents as its
source of truth. It does not create a virtual filesystem. Stored text is untrusted
evidence and cannot authorize actions or change host instructions.

## Reading progressively

1. `GET /v1/memory/summary?namespace=all&event=...` returns bounded authored summary
   excerpts and generated topic counts. Shared authors use the existing curated
   or suggested fact commands with `topic: summary`. Private authors use the
   existing memo commands with a key such as `summary.overview`. Shared suggestions
   still require classification and independent authorized human review.
2. `GET /v1/memory/index?namespace=...&event=...&topic=...` returns a page of keys,
   160-character excerpts, versions and opaque source references. This is a
   generated navigation index, not an authored semantic summary.
3. `GET /v1/memory/read?ref=...&cursor=...` reads a current source record in chunks of at most 2000 Unicode characters. Follow `next_cursor` while `more` is true. An
   outdated version returns `knowledge_stale`; refresh the index before retrying.
4. `GET /v1/memory/history?ref=...&cursor=...` returns explicitly historical
   captured source revision excerpts. Each identifies its version and capture time. `GET /v1/memory/revision?ref=...&cursor=...` reads a historical body in the same bounded chunks.
   Capture starts at migration 046, including a snapshot of then-current records;
   earlier versions are not reconstructed. `GET /v1/memory/sources?ref=...` returns actual linked conversation event IDs and 160-character excerpts, filtered to the reader's own history. The full archived event is available through the existing owner-private history reader.

Shared event precedence is identical to knowledge retrieval and is applied before
search: exact event, eligible past-event fallback, general. Results retain actual
source event and historical-fallback labels. An empty event selects general
shared knowledge. Private topics use the prefix before the first dot in a memo
key, or `notes` for an unprefixed key. Legacy records and limits are preserved. Documents have their own explicit topic and key, with `source_kind: document`; this also disambiguates a document from a legacy memo with the same key.

Generated index excerpts and topic counts are calculated from active source rows
on every call. They cannot retain old bodies after a source changes or is deleted.
Authored summaries are independent source records, not automatically synthesized
claims about other entries: edit them through the same versioned commands.
Deletion scrubs that source's captured history bodies while preserving revision
metadata. Migration 053 also scrubs matching operation receipt bodies and approved
proposal copies. Replays keep their original identity and version with
`redacted: true`; they never reapply the deleted write. Later recreated versions
remain intact. `GET /v1/memory/deletions` returns owner-private and shared deletion
generations for invalidating retained tool output without exposing private keys.
Historical Telegram conversation archives and backups have separate retention;
this is not a complete historical data-erasure mechanism.

## Search and continuation

`GET /v1/memory/search` and `/index` accept the same parameters:

- `namespace`: `shared`, `private`, or `all` (default).
- `event`, `topic`: optional exact filters.
- `q`: up to 200 Unicode characters.
- `mode`: `literal` (default), `regex`, or `text`.
- `cursor`: the preceding page's opaque `next_cursor`.

Literal search is case-insensitive and treats SQL wildcard characters literally.
Regex uses Go's bounded-complexity RE2 engine and is case-sensitive unless the
expression requests otherwise. Text search uses PostgreSQL's `simple` dictionary
and `websearch_to_tsquery`, with token conjunctions, quoted phrases, OR, and
exclusions. It is lexical full-text search, not embeddings or semantic similarity.
All modes search key, topic and body. Ordering is namespace/topic/key; no relevance
or vector score is implied.

Each search reads at most 100 candidates and returns at most 20 hits. An empty
page with `incomplete: true` is not evidence of absence: continue through
`next_cursor`. `more` indicates more candidates, not necessarily more matches.
Cursors bind the authenticated actor and normalized query; changing filters or
actor requires starting over. Search and history pages are live keyset reads,
not a stable snapshot across edits. Refresh from the first page after mutations.
Bounded result/regex work does not imply constant database work for event ranking.

The authenticated principal selects the private namespace before any matching.
References and cursors are navigation tokens, not capabilities. No API accepts an
owner override. All shared writes, review separation, optimistic versions and
actor-scoped replay semantics remain in the existing knowledge command executor.
Migration revision triggers run inside those same transactions; exact replays do
not insert a second revision. History contains only approved facts and private
memos, never pending or rejected suggestions.

## Integration and remaining scope

The host should provide only the summary/navigation surface initially, and use
index, search, detail and history reads as needed. It should use native JavaScript
for JSON processing; this API introduces no jq/gojq language or filesystem shell.
Independent synthetic composition QA on 2026-09-27 verified representative
queued provider/Sobek and EN/RU rendered flows: complete bounded reads, search
continuation, owner isolation, moderation, current permissions, deletion of
shared/private cached output and restart. Empty candidate-limited search pages
were followed to a later match. This does not establish real-model quality,
final Telegram acceptance or exhaustive concurrency coverage; see PROGRESS and
its evidence history. Larger private topic documents and trusted source links
are described below; legacy memos retain their eight-item, 512-character behavior.

Production targets Linux Docker on CPU only. This implementation uses PostgreSQL
and the Go standard library with no accelerator or new runtime dependency.
The deployment's pgvector PostgreSQL 17 image does not prove a live extension or
embedding provider is configured. No embedding model/provider has been selected
or verified, and no semantic retrieval evaluation has passed. Future embeddings
must use a CPU-feasible model or an explicitly configured remote provider.

## Larger private topic documents

`POST /v1/knowledge/actions` accepts `document_set` and `document_delete` using the
existing command envelope: host idempotency `key`, empty `event`, explicit `topic`,
`fact_key`, expected `version`, and `text` (empty for delete). Document bodies are
limited to 16000 Unicode characters, with at most 128 active documents per owner.
Updates at capacity remain possible; deletion frees a slot. Concurrent mutations
serialize on the authenticated actor. Existing replay and metadata audit handling
applies. The optional response `document` contains topic, key, body, version and
active state; model adapters should project the mutation receipt to metadata and
use bounded reads for content instead of echoing an entire submitted document.

A document with topic `summary` is an authored semantic summary. The overview
includes its bounded excerpt without injecting full documents. Generated registry
excerpts and counts always use current source rows. Both private documents and
legacy memos appear in the same paginated index/search; legacy `/me/memos` remains
unchanged. Migration 049 adds document storage and extends revision identity with
source kind. It does not convert or delete existing memo rows.

## Trusted provenance binding

`Service.ExecuteWithSources(ctx, actor, command, sourceKeys)` is a host-only domain
entrypoint. Every key must resolve to an actual archived conversation event owned
by that actor, inside the mutation transaction. Invalid or foreign keys roll back
the command. Keys are not fields of public command JSON and are never model
arguments. Suggestions retain their source links; approval transfers links to the
published revision, while source text remains visible only to its original owner.

For privacy-aware bot flows that archive after planning, the host calls
`AttachMemorySources(ctx, actor, operationKey, sourceKeys)` after archival. The
stored actor-scoped operation receipt chooses the target record and version.
Missing receipts are harmless no-ops for denied or interrupted commands. The host
constructs `tg-user-<updateID>` from its admitted update, never user/model text.
Repeated attachment is idempotent. A deleted/scrubbed revision cannot regain old
source links. Database failures remain errors and can be retried. Before attachment
there is no fabricated or guessed source link; later processing completes it.

Source reads contain references and excerpts rather than copied conversations.
An empty source list can mean that no provenance was captured or that the reader
has no access to the author's private source; it does not weaken shared fact
moderation or grant access to another user's history. Source excerpts preserve
existing sensitive-text omission and mark additional clipping explicitly.

## Serialized response and service boundaries

Search/index and history stop before the 24 KiB serialized JSON page budget as
well as the item limit. The next cursor remains before the first unreturned hit;
JSON control-character escaping cannot hide a whole page behind the host's 32 KiB
callback limit. Summary composition uses the same budget and explicitly marks
omission with `more`; continue through the topic index. Current/revision chunks
and source excerpts have separately tested worst-case encoded bounds. Knowledge
command requests alone accept up to 128 KiB, so a valid 16000-character document
still works when JSON escaping expands it beyond 64 KiB.

The bot never needs direct database access to Core memory or archive tables.
`POST /internal/memory/sources` verifies an owner-bound signed host token with the
`zns-memory-provenance` audience, accepts only an operation key and actual update
ID, and derives the archived request key server-side. Ordinary shared/memo writes
attach after successful execution; script document receipts attach after the
privacy-aware archive. Both paths preserve idempotency and source-owner privacy.
Normal user and notification tokens cannot call this service endpoint.

The same narrow host scope provides `/internal/history/archive` and
`/internal/memory/assess`. Archival retains Core sensitive-text sanitization and
suppresses sensitive/media reply bodies; the bot supplies trusted update metadata.
Assessment can only classify the actor's pending suggestion and cannot publish a
fact or assign a permission. These routes are not conversational tools. No JSON
owner override is accepted and no Core database permissions are granted to the
split bot role.

Host conversation summary batches and commits also use this service boundary.
Pass-notification archival completes in Core before the bot persists its delivery
receipt. Thus a persisted receipt always has source history and archive failures
leave no receipt. These are ordered separate commits; failure before receipt
persistence retains the already documented Telegram resend window. The archive's
owner/source-key uniqueness preserves the first archived text across retries.
