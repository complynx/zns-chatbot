# Stickers and custom emoji

Telegram stickers are active conversational messages. Their event means that the
user expresses an emotion or reaction through a sticker; it is not a request to
analyse an uploaded photo or video. The agent receives both that event type and
the sticker's visual description. A tentative `reaction_hint` helps it respond
to the conversation rather than narrate pictured objects. Description is still
available when the user explicitly asks what a sticker depicts.

Custom emoji in text and captions are replaced at their UTF-16 entity spans by
`<custom-emoji>` tags with short JSON descriptions. A sticker becomes a
`<sticker>` tag whose JSON includes `event: "sticker_reaction"`. Telegram file IDs
and custom emoji IDs stay outside conversational model input. Ordinary Unicode
emoji remain ordinary text.

Descriptions are untrusted visual interpretations. Neither an emotional gesture
nor a friendly-looking sticker establishes consent, identity, a profile answer,
payment confirmation or permission to perform a business operation. Reply
language follows the surrounding user text or conversation, not the language of
the transport annotation.

## Description catalog

Each bot has independent sticker and custom emoji catalogs. Both default to
5,000 retained entries, configured by `sticker_catalog_limit` and
`emoji_catalog_limit` (1–50,000; zero selects the default). File bytes are not
stored in these catalogs. Cached descriptions survive restarts and are reused.
An unknown entry is explicitly unknown; its unrelated Unicode fallback is never
used as a substitute description.

Each accepted occurrence adds one to a popularity score. Duplicate Telegram
updates do not increment it again. Between uses:

```text
score(now) = score(last_use) × 2^(-(now - last_use) / T)
T = clamp(k × median(reuse_intervals), minimum, maximum)
```

The catalogs calculate separate medians. Each retained symbol contributes its
latest positive interval between uses; repeated copies in the same message add
usage weight without creating extra zero-length intervals. At least ten symbol
intervals are needed. The policy is evaluated at most hourly, during intake,
using server acceptance times. Idle time alone does not inflate the median.

`symbol_median_multiplier` selects `k` (default 1). The default minimum and maximum
half-lives are one hour and 180 days, selected by
`symbol_min_half_life_seconds` and `symbol_max_half_life_seconds`.
`symbol_half_life_seconds` is the sparse-data fallback (default 30 days), which
must lie within those bounds. Zero settings select their defaults.

On a half-life change, retained immutable input history reweights existing
scores. On a cache miss, that history also recovers previous usage weight, so a
frequently reused newcomer can enter a full catalog instead of repeatedly
losing its count on eviction. History deletion limits what can be reconstructed.
Ranking uses logarithms to avoid underflow when comparing old entries.

## Separate visual-description procedure

`cmd/shuttle-symbols` explicitly processes one retained unknown symbol. It uses a
dedicated Loom account and a separate chat, without a user's conversation or
memory. The account needs image/file inspection and its own model/billing policy;
do not grant it business authority. Run creation uses the configured account's
model preferences. Normal intake records an unknown catalog entry; a separate
explicitly enabled worker owns paid description work.

Before preparing a new agent request or control input, Shuttle waits for missing
catalog descriptions for up to six minutes from the original durable intake time.
The task snoozes without starting a model request; other conversations can proceed.
The `choose_sticker` indicator stays active during this bounded wait and active
symbol-task processing. Other processing and queued deliveries use `typing`.
Both signals refresh every four seconds and stop when no eligible work remains.
Retries and restarts retain that deadline. Previously prepared request bodies are
not rewritten. At the deadline, unresolved descriptions remain explicitly unknown;
that does not mean the image was lost or require the user to resend it.

`-watch` polls retained unknown entries with up to four concurrent procedures
(`-concurrency` accepts 1–4, default 4). A generation occupies one slot; duplicate
uses share its catalog result. Each slot persists its original run before work.
The original serial active pointer remains slot zero. Reducing concurrency with
occupied higher slots requires reconciliation. A durable pointer restores the
original run even if its catalog entry was
evicted before a graceful restart. An old completed result remains in its journal
for reconciliation instead of being applied to a replacement generation.
Its private root holds one journal per catalog generation; completed entries are
not selected again. It does not use a user's conversation or credentials. The
protected service/descriptor token files can be rotated between steps. Transient
read failures retain their original generation and journal, with a durable retry
deadline. Backoff grows from two seconds to five minutes; a longer Telegram
`RetryAfter` takes precedence. Temporary catalog database-read failures pause the
affected discovery slot without stopping healthy active slots. Permanent failures
and unknown mutation outcomes receive a private blocked marker for reconciliation;
they do not start replacement work or hold other entries. A stale process lock after a crash
also needs reconciliation before restarting. Catalog eviction leaves journals
intact and cannot make an old result overwrite a new generation.

Transient Telegram media reads receive at most three attempts with a short,
bounded backoff. Explicit missing/access-denied files are not retried. This
applies only to reads; exhausted transient reads return to the worker's durable
backoff. Acknowledged uploads remain journaled, and unknown Loom
chat/upload outcomes still require reconciliation.

For technical diagnostics, set `SHUTTLE_LOG_LEVEL=debug` on Shuttle or the
descriptor (`-debug` also enables descriptor diagnostics). Logs include task or
procedure correlation, catalog counts, wait deadlines, preview positions,
hashed file keys, attempts, durations and safe transport outcomes. They exclude
message text, media bytes, credentials and credential-bearing URLs. Enable log
rotation when using debug for an extended acceptance run.
The configured OTLP exporter also receives descriptor-step and media-read spans,
with per-attempt results. Catalog lookup and description-wait events use the
message processing span. This telemetry contains technical counts and outcomes,
not message text, source file IDs, media or credentials.

The procedure requires authenticated capability negotiation for Loom's
`native_files` execution profile and persists that profile in its creation
request. Missing support stops before chat creation or uploads. Legacy journals
with unrestricted requests require reconciliation; their requests are never
rewritten or replayed as restricted runs. See
[Loom PR 45](https://github.com/complynx/loom/pull/45) for the isolation contract.

Use `-actor-token-file` alone for a configured own-subject service account. Its
effective rights intersect verified IAM roles with the configured permission
ceiling. Existing delegation may instead supply `-subject-token-file`. Neither
mode grants business/admin authority to bypass discovery. The native profile
excludes business discovery and calls, personal memory, external source refetch
and paid transcription. Upgrade all Loom execution workers together before use.

The procedure retrieves actual Telegram media, not the fallback Unicode symbol.
Targets retain their original bytes, including WebM, TGS/Lottie and animated
raster containers. A static thumbnail never substitutes for the target's
animation. The descriptor must inspect composed timeline frames through Loom's
file tools. The set sheet contains static previews only. Missing codec support
is unavailable evidence, not proof of a still image or motion. The animation
worker contract is provided by
[Loom PR 41](https://github.com/complynx/loom/pull/41); consumer acceptance must use
its API and worker builds together.

When a set is available, a bounded contact sheet includes every member in
Telegram order. It retains layout context for writing, technical signs,
ornaments, dividers, vignettes and multi-piece objects. A target index identifies
the member being described. Set context is evidence, not proof that neighboring
members necessarily form one object. The description records a category,
visible literal text, supported composition hints and a tentative reaction.
The descriptor also receives the set's available name and title as untrusted
context. Bot API does not provide a separate set description. Pack metadata
alone cannot establish character identity or override the procedure instructions.

The conversational input preserves adjacent tags, whitespace and line breaks.
Fragments can therefore be interpreted together in the actual message order.
Ornamental or textual custom emoji must not all be treated as emotions.

The private procedure journal fixes set membership and stores acknowledged chat,
artifact and run receipts. Known upload checkpoints resume without uploading
earlier assets again. Unknown non-idempotent chat/upload outcomes require
reconciliation. Run creation reuses the original key and request. Catalog
eviction does not erase the journal, and a completed old generation cannot
overwrite a re-admitted entry. Completed description application is idempotent.

## Outbound compatibility

The agent must use ordinary text and Unicode emoji in user-visible responses.
New conversational runs require authenticated `GET /v1/capabilities` to advertise
`output_validation.version: 1` and `disallow_custom_emoji: true`. Shuttle then
persists `output_policy: {disallow_custom_emoji: true}` in the original creation
request. Unsupported capability waits for service compatibility; it never falls
back to an unrestricted run. Authentication errors retain normal authorization
recovery.

Loom rejects unsupported final text, questions, choice labels and approval
presentations before public events. The same run receives a validation error and
can rewrite the presentation without restarting completed business work. A new
correction inference has normal billing. Exact consent-bound confirmation text
cannot be rewritten: the module must supply a compatible confirmation contract.

Saved creation requests from before this policy keep their original bodies and
idempotency keys. Existing runs are not retroactively restricted. Upgrade all
executors before enabling new restricted runs and drain them before rollback.
The contract is described in
[Loom output validation](https://github.com/complynx/loom/blob/871f6fd6dca5f925475adbeba1ee133403e13213/docs/api/output-validation.md).
Controlled Linux consumer acceptance covers final and question-label correction
within their original runs and delivery to mock Telegram. It does not establish
actual-model style or real Telegram acceptance.

## Focused QA

Check known and unknown descriptions, duplicate updates, UTF-16 surrogate
boundaries, captions, ordered adjacent fragments and line breaks. A sticker
must create or steer an active run and must not trigger the avatar path.

Check independent caps, decay, sparse medians, bounded adaptive periods and
newcomer promotion after eviction. For the separate procedure, cover restart
after acknowledged uploads, unknown upload outcomes, exact run replay, invalid
description JSON and eviction during work. Actual-model emotional responses and
outbound correction are separate acceptance from controlled transport tests.

## Optional message reactions

This private-chat transport does not receive user reaction updates. Outgoing bot
reaction receipts do not establish whether a user reacted to a message. The agent
must state that it cannot see those reactions rather than report their absence.
Telegram's [Bot API update contract](https://core.telegram.org/bots/api#update)
requires chat administrator status and explicit subscription for reaction updates.

New conversational runs advertise a bounded ordinary-emoji repertoire through
Loom's `interaction.reactions.emoji`. The agent may use `message_react` when a
human response would fit: encouragement, empathy, shared excitement or a bashful
reaction to a correction. It need not react to every message. A reaction does not
replace a necessary answer and never grants consent or business authority.

Shuttle consumes authenticated chat history and maps its `message_seq` through
durable intake and delivery receipts to the exact Telegram message. Chat sequence
numbers, SSE cursors and Telegram message IDs are distinct. Reactions to a split
assistant response target its first delivered chunk. External or unmapped chat
messages are not guessed from their text.

The current assistant projection and its reaction watermark take precedence over
older deltas. Telegram receives at most one advertised ordinary reaction per
message; an empty selection removes it. Custom and paid reactions are excluded.
If an outgoing target has no acknowledged Telegram delivery yet, its projection
is retained separately until the receipt is known. It does not hold the history
cursor or other targets.

Reactions have a separate durable delivery state. A known rejection does not
block the text answer. A lost acknowledgement freezes only that reaction target
as `unknown`; restart or a newer desired selection does not retry it. Its exact
attempted selection, version and watermark remain available for reconciliation.
Local user access is checked again after credential verification before sending.

Focused QA covers exact incoming targets, delayed outgoing receipts, replacement,
removal, watermark replay, restart after an uncertain send and local revocation
during credential refresh. HTTP-peer tests establish Shuttle behavior, not actual
model judgment or real Telegram acceptance. Incoming user reaction updates are
not yet projected into Loom by this consumer.

Static target and contact-sheet images use direct native attachment. Telegram
animation/video flags accompany new descriptor requests; a video stream reported
for a static PNG/WebP is not itself evidence of animation. Animated originals
still require decoded frame inspection rather than thumbnail substitution.

Descriptions may identify a recognizable character or mascot by name and source
when visible traits support the match. Tentative matches remain explicit guesses;
otherwise the description records appearance without inventing an identity.
