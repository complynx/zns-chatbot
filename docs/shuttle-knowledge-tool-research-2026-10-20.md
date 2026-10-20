# Knowledge retrieval and agent tool design, 2026-10-20

Status: source research and initial design decision, not implemented acceptance.
This supplements the [knowledge design](shuttle-knowledge-bases.md). It does not
change the bot/Loom ownership boundary or certify retrieval quality on our data.

## Observed approaches

[AgenticRAG, submitted May 2026](https://arxiv.org/abs/2605.05538) describes
iterative search, find and open tools over an existing enterprise search backend,
with stable references and bounded reading windows. It evaluates public retrieval
and QA datasets, rather than our event knowledge. Its ablation reports that
semantic find did not improve its average retrieval result relative to a literal
fallback. This supports testing simple document navigation before adding a
second inference/semantic layer; it does not prove that semantic retrieval is
unnecessary for our Russian/English corpus.

[Anthropic context engineering, September 2025](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
describes loading relevant material just in time through lightweight identifiers.
[Tool design guidance](https://www.anthropic.com/engineering/writing-tools-for-agents)
emphasizes meaningful tool boundaries, concise responses, pagination and realistic
agent evaluations. These principles favor a few discoverable navigation methods
and bounded passages, rather than returning all articles to each agent turn.

[Advanced tool use, November 2025](https://www.anthropic.com/engineering/advanced-tool-use)
separates discovery of tool definitions, programmatic orchestration and usage
examples. Tool discovery is different from searching knowledge content. Keep
zinusya knowledge methods in the existing manifest; do not introduce a competing
tool registry. Batch independent read/search requests where bounded batching saves
round trips. Tool schemas and examples should explain current versus historical
content, explicit filters and exact revision references.

[OpenAI Retrieval documentation](https://developers.openai.com/api/docs/guides/retrieval)
describes metadata filtering, ranking and lexical/embedding result fusion. This
is a useful future retrieval option, not a requirement to use hosted storage.
Shuttle remains the authority for article versions, access and moderation.

[QMD](https://github.com/tobi/qmd) demonstrates a local Markdown-oriented design:
collections/context, keyword and semantic retrieval, fusion/reranking, document
reads and metadata discovery. Its Node/native-model runtime and separate index
would add deployment and authorization work to this Go/PostgreSQL application.
Borrow the bounded collection/navigation interface first. This is a reference,
not a selected dependency, installation or claim that its defaults fit our corpus.

[Microsoft GraphRAG](https://microsoft.github.io/graphrag/query/overview/)
distinguishes entity/local questions from corpus-wide questions answered using
generated community reports. Its global map-reduce search is explicitly resource
intensive. Do not build graph extraction or generated corpus summaries merely to
answer a venue address or read current event rules. Reconsider graph methods only
for a demonstrated need for historical relationship/corpus-wide analysis.

Local schema probes now record concrete Russian stemming misses (`танец` versus
`танца`, `фестивали` versus `фестивале`). These remain retrieval-quality cases,
not fixed by changing a smoke query. See the
[foundation evidence](shuttle-knowledge-foundation-2026-10-20.md). Query
reformulation and any future semantic layer must be evaluated on these examples.

## Agent-facing choice

The implementing agent's preference is a design judgment informed by using
repository search, web search/open/find and bounded read tools. It is not a
measured claim that one interface wins for every model.

Preferred sequence: overview when the subject is unknown; search for candidate
articles; read the relevant section; find an exact term inside a long article;
follow source/revision references to resolve conflicts. Keep common reads easy
to discover. Do not force the agent to choose embedding models, chunk sizes,
ranking weights or SQL syntax for ordinary questions.

A useful result needs more than a similarity score: recognizable title, snippet,
collection, event origin, current/historical classification, publication revision,
updated/approved provenance, exact reference and pagination/truncation metadata.
Score is a ranking hint, not the probability that a statement is true. An absence
of results does not authorize an invented answer. A failed filter is an error,
not permission to return unrelated events.

Use deterministic article/revision/section references, preserved across bot/Loom
restarts. An index hit never bypasses permission or current revision checks.
Search and section reads exclude pending suggestions. Future optional semantic
indexes must remain derived and must be checked against current canonical data.

Editing needs readable previews/diffs of the exact proposed revision, an expected
base revision, and a distinct moderator business decision. A user pressing an
agent consent choice cannot impersonate a moderator. Keep content retrieval and
publication tools separate; retrieved Markdown cannot grant tool authority.

## Initial implementation and evaluation

Implement PostgreSQL lexical/full-text search over published material and
structured title/topic/event filters, with bounded multiple-query input and
literal in-article find. Use the reactive Loom agent for query reformulation and
synthesis; do not start an additional hidden retrieval agent inside Shuttle.
Leave embedding/hybrid/reranking changes to evidence from retrieval acceptance.

Evaluate complete questions and citations, not just successful HTTP calls:

- Current event venue/rules, even when an old event has similar text.
- Russian question and English terminology; alternate names and spelling.
- Several formulations that lead to the same article without duplicate output.
- Long article where the relevant clause is outside its initial excerpt.
- Contradictory old and current revisions with correct source attribution.
- No evidence: agent clarifies or admits the gap, without invented facts.
- Pending suggestion/private legacy transcript never appears in search results.
- A normal proposer and a distinct event admin/general moderator complete review.
- Event end and restart preserve references and historical classification.

Record answer/source correctness, retrieval coverage, wrong-event/historical
confusion, call count, response bytes, latency and token consumption where the
provider exposes it. Freeze a small held-out question set before tuning. Add
hybrid retrieval only if it repairs observed misses within our resource budget.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
