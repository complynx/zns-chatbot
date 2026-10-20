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

## Remaining acceptance

Connect the native frontend and Telegram `/knowledge` entry. Add actor-owned
operation discovery and reactive agent context, an event directory independent
of registration/module validity, moderator management UI and durable reviewer
notices. Known-operation recovery already returns exact terms; selecting those
operations across channels/devices remains open.

Then perform native HTTP authorization and owned multi-user real-IAM/Loom/mock-
Telegram acceptance, including both native/agent continuation directions and
notification recovery. The corpus contains no fabricated factual seed material.
The new source is not installed, pushed or declared production-ready.

No production-readiness or deployment claim is made.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
