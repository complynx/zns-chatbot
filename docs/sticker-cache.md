# Canonical sticker and custom emoji description cache

Cache implementation: `platform/internal/stickercache`, migrations
`018_sticker_cache.sql` and `021_sticker_cache_owner.sql`. The forward migration
preserves cached data while moving it to the bot schema and granting only the
bot role access; the bot does not gain access to core business tables.
`stickerassets.Service` connects Telegram, the isolated decoder, a context-free
model description and this cache. Its tests use synthetic models. This is built
infrastructure, not yet accepted Telegram functionality.

Bot configuration: `STICKER_WORKER_URL`, `STICKER_WORKER_SECRET`, optional
`STICKER_CACHE_CAPACITY` (default 5000), and `STICKER_CACHE_HALF_LIFE` (Go duration,
default 168h, minimum 1s). All processes sharing the cache must use equal options.
Each message resolves at most eight distinct assets to bound model work/context;
repeated emoji share one observation and one popularity hit. Additional distinct
assets are explicitly reported as omitted, never described from placeholders.
Failures are unavailable observations and are not cached. Model cache version
changes invalidate descriptions when the model or canonical prompt changes.

## Contract

Construct `stickercache.New(pool, Options{...})`, then call
`Get(ctx, Key{Kind, AssetID, DescriptorVersion}, processor)`. Kind is `Sticker`
or `CustomEmoji`. Use an immutable Telegram asset identifier (stable sticker
`file_unique_id` or custom emoji ID), never a message, user or temporary download
identifier. `DescriptorVersion` includes all model/prompt/schema changes that
invalidate a description. Different versions occupy independent entries; old
versions compete for the same capacity and leave only metadata after eviction.

The processor receives the asset key and a cancellation context. It must fetch
and interpret the actual static image or animation, then return a self-contained
canonical asset description. A closure can carry a download capability but must
not introduce captions, sender identity, surrounding chat or user-specific
interpretation into the description. Those belong in downstream per-message
context. This package checks size, UTF-8 and nonblank text; it cannot prove
semantic independence from user text. The adapter must enforce that contract.

Cache hits never invoke the processor. Failure, cancellation or invalid output
does not store a description; a later access retries. A description is limited
to 8192 UTF-8 bytes in both Go and PostgreSQL. Asset keys are limited to 512 bytes
and descriptor versions to 128 bytes. No media bytes, private message text or
download URLs are stored here.

## Lazy popularity and admission

Defaults: **5000 descriptions**, **seven-day half-life**. Zero options select
those defaults. Capacity must be positive and half-life at least one second.
All processes sharing the table must use the same options. An injected clock is
available for deterministic tests; production defaults to `time.Now`.

No timer, sweeper or periodic decay job exists. On each access, one row changes:

`score = old_score * 2^(-max(0, now - scored_at) / half_life) + 1`.

The timestamp advances monotonically; a backwards clock does not amplify old
scores. Hits do not scan or rank the cache. A failed processing attempt still
counts as an access to the asset's popularity metadata.

At 1022 elapsed half-lives, the decayed contribution is treated as zero in all
four access/admission/ranking expressions. This is the IEEE-754 minimum-normal
exponent boundary and avoids PostgreSQL numeric-to-double underflow after long
idle periods. Service-generated stored scores are at least one. This explicit
negligible-tail approximation also applies while processing a slow miss; it
does not add maintenance work or change ordinary decay.

On successful processing, one short admission transaction:

1. If full, rank existing resident descriptions by scores decayed to the same
   current instant. Retain the best `N-1`; set evicted descriptions to NULL.
   Deterministic asset-key order breaks eviction ties. The new candidate is
   always admitted, even if it previously had a low score.
2. Among surviving residents, sort current decayed scores descending. Select
   zero-based index `floor(survivor_count / 2)` as the threshold. Set the new
   asset's score to the greater of its own current score and threshold plus one.
   Use the next representable float above the threshold if adding one rounds
   away. An empty cache starts at one.
3. Store the canonical description. Its rank at admission is in the first half:
   at most `ceil(final_resident_count / 2)`. Higher historical popularity remains
   intact. Hits later apply ordinary decay-plus-one without another boost.

This is an **approximate popularity cache with compulsory admission and a
first-half admission boost**, not the exact globally most popular N assets.
Eviction computes scores lazily without rewriting every resident's timestamp.
Metadata survives eviction, allowing a returning asset to retain popularity.

The limit bounds non-NULL descriptions, not metadata rows. Each evicted row keeps
only its bounded asset/version key, score and timestamp. Metadata currently has
no pruning or retention deadline and can grow with unique asset/version count;
a future retention policy must be an explicit product decision. Media storage
does not grow because this cache never stores media. Configuration changes are
coordinated application configuration, not runtime administration: changing the
half-life reinterprets elapsed time since each row's last access; decreasing N
trims at the next successful admission. Do not claim an immediate resize.

## Concurrency and failure boundaries

Each call acquires one pool connection and a session advisory lock derived from
the full asset key. The lock serializes same-key misses across processes and
survives no database-session loss. The callback runs without a transaction or
global lock, while retaining that one connection. Pool capacity bounds active
and waiting cache requests. Processors must honor cancellation and must not
acquire another connection from the same pool, which could deadlock exhaustion.

One-row access statements take a short shared transaction advisory lock;
admission uses its exclusive form. This permits concurrent hits but prevents
hits from changing the score ordering between eviction/boost and publication.
The processor itself never holds this lock. Different-asset processing can run
concurrently; publication is serialized to preserve the capacity bound.

Unlock uses an independent three-second cleanup context. If lock acquisition
fails or unlocking cannot be confirmed, the connection is removed from the pool
and closed; a possibly locked connection is never returned for reuse. Transaction
rollback cleanup is also bounded. PostgreSQL releases locks on session loss.

Queued same-key calls reuse the first successful resident description. As with
any bounded cache, another asset can evict it before a queued access obtains its
lock; that access is then a legitimate new miss. Likewise processor failure
allows the next waiter to retry. There is no durable result beyond the configured
description capacity and no exactly-once external model guarantee after crashes.

## Verification

`platform/integration/sticker_cache_test.go` uses isolated real PostgreSQL
databases and testify. It covers concurrent misses across cache instances,
restart hits, descriptor versions, capacity, lazy decay with a fixed clock,
metadata retention, first-half admission at full/partial capacity, preserved
high historical score, failure/retry, byte limits and cancellation/lock cleanup.

Run from `platform` with the local test DSN in `TEST_DATABASE_URL`:
`go test ./integration -run TestStickerCache -count=1 -v` and the pinned
`tools.local/golangci-lint.exe run ./internal/stickercache/... ./integration/...`.
Independent Code QA and Telegram Functional QA remain required for acceptance.
