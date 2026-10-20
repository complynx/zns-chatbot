# Knowledge foundation evidence, 2026-10-20

Status: store, agent API and bounded archive maintenance are now in the working
branch. They are not installed on the main stand. Native forms, moderator
notifications and end-to-end knowledge acceptance remain open.

## Applied implementation checkpoint

The domain and additive migration65 are applied to source. The store implements
published retrieval, suggestions, independent review, named moderators, exact
operation receipts and event archival. The agent API exposes these operations
under `zinusya_knowledge`; the source manifest is mvp79. The installed stand is
still schema64/mvp78 and has not received these changes.

The final isolated Linux/race PostgreSQL gate v5 passed nine roots and18 terminal
cases, with no failures, skips or OOM. It covers independent review, event scope
and archival, revocation, native/agent handoffs, typed operation recovery and
maximum supported plain/JSON-escaped content. Its own resources were archived
and removed, the tested source stayed unchanged and the main stand was preserved.
The private post-test archive is856 bytes, SHA-256
`ae0b6335ca2cf452ff16345f0dd68e548a40d106e0c1af41955d6dd3c2e6ed0d`.
This proves archive decoding, not restoration of a running service.

The earlier isolated five-root gate v2 passed all five roots,
without skips or OOM. Its own containers, volume and network were removed; the
main stand and tested source were preserved. The post-test database archive was
fully decoded, but this is not a restored-runtime or production-backup claim.

V1 remains failed: four roots passed and one compared internal Go time.Location
representations after JSON receipt restoration. The corrected test compares the
public JSON receipt, including timestamps; its diff has a clean independent
review. The failed run's logs and private archive remain retained.

Native authenticated HTTP routes are now registered in source. They share the
same operation/proposal IDs and transactional browser-session guard as the agent
store. Known-operation status returns the original preparation and distinguishes
completed, prepared, expired and unknown. Expired requires an existing preparation
and an operation-serialized observation without a receipt; unknown is nonterminal.
Completed receipts take priority over expired original consent. Current mutation
permissions and versions are always checked again before an effect.

The strict combined lint gate for API, DB, domain, task and integration packages
passes with zero issues. API unit tests pass after native routes/status wiring.
The preceding v3 gate passed eight roots/15 cases. Two full store/API review
rounds found and fixed two substantive issues, then returned no actionable
findings in fresh reviews: native moderator changes require explicit enabled
and version values, and serialized preparation/receipt ceilings cover supported
content sizes. Focused native-field and size-bound reviews also passed.

The frontend remains an ignored draft. Eleven mock-HTTP/DOM Node roots passed, but
this does not prove a real browser, native HTTP authorization or Telegram behavior.
The draft now consumes the authoritative status contract. Native
operation/event discovery, reactive agent context, moderator notifications and
complete native/agent/mixed acceptance remain open.

The sections below record the earlier draft/component checkpoint and its limits.
The [design](shuttle-knowledge-bases.md) and [tool research](shuttle-knowledge-tool-research-2026-10-20.md)
passed two design-review rounds. Native-only/mixed parity and immediate effective
historical discovery were added following review. No production service or Loom
developer stand was changed.

## Earlier draft implementation

The ignored `knowledge-draft-v1/` contains content/search normalization, bounded
heading outlines, exact section/line reads with Unicode-safe byte continuation,
and literal in-article find. It uses the existing Goldmark and UUID dependencies.
The earlier focused Go verification passed11 roots/35 terminal cases with no skipped
behavior cases. The generated sqlc package compiles but has no test cases; its
package-level no-tests report is not behavioral acceptance.

Three navigation review rounds found and fixed quadratic duplicate-heading
anchor generation and empty ATX headings omitted from section boundaries. A
20,000-duplicate/collision case and empty-heading LF/CRLF/EOF cases now pass.
A CR-only probe did not establish a mismatch: the underlying parser itself sees
one line/heading. It was not used to expand the accepted format contract.

The additive65 schema and read/search/revision query drafts preserve article
identity, published revisions, event provenance, independent user suggestions,
explicit moderators and exact-operation audit state. Scoped queries classify
ended event articles as historical before the physical sweep. The migration and
final domain/query diff have clean independent reviews; sqlc generation passes.
Store execution authorization, ranking/result excerpts and archival are not yet
wired to a running service.

## Earlier PostgreSQL scope and retained failures

Fresh owned PostgreSQL17 fixtures, isolated from the main stand, verified the
existing64 migrations plus proposed65, empty65 rollback, generated Russian/English
and literal search, unpublished suggestion exclusion from the published article
index, and refusal of populated65 rollback with the article retained. The v7
fixture exits0 with no OOM and is removed. These are schema-level checks; they do
not prove API permissions or end-to-end agent retrieval quality.

Earlier fixture runs remain failed. V1/v2 lacked the transactions that Goose
normally supplies to existing migrations; v2 retained the exact LOCK TABLE error.
V5 used an unsupported SHOW parameter in diagnostics. V3/v4/v6 expected Russian
word forms to share a stemming result and failed that assumption. Direct v6
vectors establish real misses: `танец` versus `танца`, and `фестивали` versus
`фестивале`. V7 tests the supported `фестиваль` query; it does not repair or erase
these misses. Keep them as held-out retrieval-quality cases for query
reformulation and possible future hybrid retrieval. Do not claim arbitrary
Russian inflections are handled by PostgreSQL stemming.

All seven schema-fixture containers were removed; they used disposable tmpfs
storage with no production copy. Their scripts and private evidence directories
remain ignored under `.cache/shuttle-work/knowledge-schema-qa-v*/`.

## Size-bound reproduction and correction

V4 remains failed: both maximum-content variants reproduced real PostgreSQL
check failures. Plain maximum content reached the duplicated approval receipt
limit; JSON-escaped content reached the preparation-document limit. Their logs
and decoded private database archive remain retained, and all own resources were
removed. These failures were not reclassified as passing.

Migration65 has not been installed on the main stand, so its source ceilings
were corrected before installation: sources128 KiB, preparations1 MiB and
receipts2 MiB. This accounts for JSON escaping and the two complete content
copies in an approval result. Article and source field limits, HTTP1 MiB limits,
foreign keys and populated-rollback refusal stay unchanged. V5 then passed both
variants through suggestion, approval, publication and receipt replay.

## Event discovery and continuation follow-up

The overview now includes active events with no articles, independent of passes,
registration and module availability. Each page contains general/history and at
most20 active events. Pass `next_event` as `after` to continue; the general row
repeats. `truncated` can also describe the bounded topic preview, so it does not
always imply another event page. Archived origins stay historical after an event
is rescheduled. Original article provenance is preserved.

The agent has actor-owned `operation` and `recent_actions` reads. Native clients
have `recent_actions` and the existing per-verb status endpoints. Recent discovery
returns at most five metadata-only locators, not an exhaustive operation history.
Exact normalized arguments come from operation status. This permits discovery of
another channel's preparation without treating past activity as new consent.

Existing reactive tasks include recent knowledge action locators created before
that task's immutable creation time. Task ownership and the actor's current
bot/organization/subject binding are checked. Later actions do not enter that
original task on retry. Historical metadata is marked untrusted and never creates
a new agent run. Confirmation still checks current authority, versions and expiry.

The follow-up owned Linux/PostgreSQL17 race gate passed12 roots/21 cases, with
zero failures, skips or OOM. It repeats the prior nine foundation roots and adds
empty-event pagination/isolation, rescheduled-archive provenance and task owner/
cutoff/revocation checks. The private database dump was fully decoded before all
fixture containers, network and volume were removed. Frozen source remained
unchanged and the primary stand was preserved. Host API/db/domain/task tests and
the combined lint gate passed. Independent DB/task and API/context reviews found
no actionable defects. One earlier review incorrectly applied pre-Go1.27
composite-literal restrictions; the Go1.27.2 compiler and current linter accept
the promoted-field initializer used here.

These tests exercise store behavior and API projections. They do not establish
native HTTP/session, actual Loom prompt consumption or real Telegram acceptance.
The subsequent native UI change is not included in this PostgreSQL source gate.

## Native client source

The Mini App now includes a knowledge section and accepts the `#knowledge`
launch target. It uses existing authenticated requests, busy/account/language
lifecycle hooks and static-asset security headers. It adds no frontend framework
or Markdown HTML renderer: article text and source references render as text.

Users can browse/search, read current or old revisions, suggest articles and
corrections, inspect their suggestions and decisions, and select available empty
events across overview pages. Authorized editors can prepare publication; an
independent reviewer sees proposed/current text before approving or rejecting.
An active review locks navigation so another suggestion cannot replace the
comparison covered by consent.

Recent actor-owned requests can be selected across channels for the supported
suggest/edit/review workflows. Selection reads original server terms and requires
fresh consent. It never creates a guessed preparation. An unknown lookup remains
recoverable across reload. A completed receipt wins over expiry. The local
journal stores communal content and operation metadata per user/tab, not tokens.
A first definitive preparation rejection returns to the editable form;
any earlier uncertain attempt retains its original identity. The attempted marker
is persisted before dispatch to preserve this distinction across reload/crash.

Eighteen focused VM/DOM/mock-HTTP behavioral tests pass for this client. The
combined run with the32 existing Mini App lifecycle cases passed50 cases. The API
package/static-asset tests also pass. These are source-level browser simulations,
not a real browser, native HTTP or deployed Telegram acceptance result.
The independent UI review loop ended with no actionable findings. It fixed
blocked forms after definitive preparation rejection, mismatched suggestion
pagination, changing review context during consent, and hidden publication/
review metadata. The final metadata patch reran all18 knowledge cases.

## Remaining acceptance

Add the Telegram `/knowledge` command/menu entry, moderator management UI and
durable reviewer notices. The native client now supports event and actor-owned
operation selection for article/review workflows. Moderator assignment recovery
is not yet exposed by that interface; recent assignment entries are metadata
only. Prove continuation across channels/devices against real HTTP and IAM.

Then perform native HTTP authorization and owned multi-user real-IAM/Loom/mock-
Telegram acceptance, including both native/agent continuation directions and
notification recovery. The corpus contains no fabricated factual seed material.
The new source is not installed, pushed or declared production-ready.

No production-readiness or deployment claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
