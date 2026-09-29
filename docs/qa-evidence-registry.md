# QA evidence retention and reconciliation

Updated 2026-09-29. This maintenance register is not an acceptance report. The
neutral [scenario inventory](fqa-scenarios.md#global-acceptance-inventory) is the
reviewer-facing entry point. The nine-case machine-readable Stage B seed remains
scoped to Stage B; it is not the complete global acceptance matrix.

## Evidence ownership

`PROGRESS.md` records current work and accepted scopes. `PROGRESS_HISTORY.md`
retains historical stage decisions. Original reports in `qa.local` retain exact
candidate attribution, failures, limitations and independent verdicts. Avoid
copying their narrative into additional progress documents. This register links
the records and records retention decisions; it does not replace their evidence.

The local inventory is under `qa.local/qa-evidence-cleanup-20260929`:

- `report-inventory.csv`: pruned Markdown paths, sizes, SHA-256, candidate domain
  families and retention decisions.
- `case-routing.csv`: original narrative rows with file/line attribution and
  suggested families. These are **unreviewed routing suggestions**, not validated
  cases. Headings, findings and limitations can also appear as rows.
- `duplicate-reports.csv`: exact byte duplicates, retained until all snapshot and
  reference obligations are discharged.
- `inventory-summary.json`: original scan scope; `final-summary.json` records the completed maintenance snapshot.
- `REPORT.md`: maintenance outcome, protected paths and cleanup limits.
- `batch-*-reconciled.csv`: completed manual report-to-case batches. These refine
  the automatic routing index; they do not change original candidate verdicts.
- `archive-manifest.json`: lossless archive lookup with original, compressed and
  independently restored SHA-256 values and exact byte sizes.

These files can contain historical findings. Keep them local and do not include
them in a source-blind Functional QA handoff. No secret-bearing environment dump
is needed for the inventory.

## Manually reconciled historical matrices

| Original evidence | Neutral families and preserved case details | Status treatment |
| --- | --- | --- |
| `qa.local/identity40-fqa/matrix.md` | `qa.identity`, `qa.browser`, `qa.localization`: trusted message/callback and verified Mini App first contact; bot-first/browser-first provider-subject convergence; concurrency/replay/restart; unverified synthetic email; ambiguous/outage retry; banned/cannot-book policies; no email adoption/conflicting subject/removed link; prepare-only has no local writes. | Original matrix says preparing; it is not a passing execution report. Later scoped acceptance remains separately attributed. |
| `qa.local/modeltools-fqa/matrix.md` | `qa.model-settings`, `qa.authority`, `qa.localization`: all twelve rows, including disjoint own/others/global/grants, cached-call revocation, current-vs-next request settings, inheritance/reset/author restoration, changed target/stale version, bounds/privacy and restart/replay. | Original matrix says NOT RUN. It does not inherit a later model-tools candidate's pass. |
| `qa.local/architecture-stage-b-repair6/functional-qa-affected/review/MATRIX.md` | `qa.authority`, `qa.history`, `qa.knowledge`, `qa.massage`, `qa.localization`: event A revoke while B persists; paged complete/empty reads; held and transformed/copied results; derivative effects; role restoration does not resurrect retired results; practitioner/neutral empty fixture; restart and persistent provenance. | Requirements-only matrix. Empty reads, denials and absent observations remain distinct. No whole-B acceptance inferred. |
| `qa.local/functional-product-pass1/report.md` | `qa.orders`, `qa.passes`, `qa.knowledge`, `qa.script`, `qa.runtime`, `qa.authority`: cash confirmation, missing-profile guidance, memo owner isolation, language refresh, JS host-global absence, restart persistence versus interrupted side-effect replay. | NOT ACCEPTED for full assigned scope. Retry429 lacked correlated completion evidence; callback toast was not captured; root-relayed DOM was not direct screenshot inspection. These limitations remain in the original report. |
| `qa.local/composed-runtime-functional/global-fqa-coordination.md` | Lead-owned fixtures/restart/permission barriers and disjoint actors; exact frozen identity; source-blind slices with correlated evidence. | Future coordination plan, not evidence that parallel acceptance ran. |
| `qa.local/synthetic-cleanup-20260929-confirmed/REPORT.md` | Operational cleanup provenance: exact102 obsolete B containers removed, protected66 baseline containers unchanged; volumes/networks/images preserved. | Completed container-cleanup evidence only. It is not product acceptance or authorization to delete remaining snapshots/data. |

## Reconciliation coverage

The 2026-09-29 historical inventory is reconciled: **1,082 distinct documents** have per-document mappings in [reconciled-reports.csv](../qa.local/qa-evidence-cleanup-20260929/reconciled-reports.csv). This includes all 170 initial Functional/FQA documents, all 337 initial Code QA documents and 268 developer/research reports, then 177 additional reports, 89 contract/setup claim documents, 27 embedded checkpoint records, 13 historical documentation checkpoints and the explicitly requested protected C7 Functional report.

Review depth is explicit: 952 initial/expanded reports were read in full; eight historical documents and the C7 report were also read in full. The remaining 121 records were reviewed as obligation/outcome paragraphs in contracts, setup and checkpoint documents. This is not automatic keyword acceptance. Unique behavioral/security obligations, expected outcomes, locale/input, permissions, retry/restart and provenance dimensions were added to the neutral matrix. Source-only, unit, PostgreSQL, model-probe, helper and Functional proof remain separate check types.

The remaining initial 415 Markdown paths are classified in `final-classification.csv`: 13 reviewed checkpoints, 81 current reference/contract documents and 321 retained source/baseline documentation copies. Another 277 supplementary contract/reference documents are classified in `supplemental-classification.csv`. All existing report links extracted from these current docs and the 155 unique report references in `PROGRESS_HISTORY.md` resolve to reviewed records. Source/dependency trees were pruned, not recursively hashed. Initial routing/pending lists are historical scan artifacts; `final-summary.json` and the consolidated mapping are authoritative for maintenance completion.

No old report in this declared inventory remains unclassified. Active workers can produce new evidence after this snapshot; those reports require their own gates and are not silently covered here. Missing product coverage is still open. C7 is **INCOMPLETE, no confirmed defects**: same-resource contention, after-effect loss, held in-flight restart/assessment cancellation, complete registration/payment/moderation and all locale/input cells remain unproven. Operator history deletion does not prove a user-facing deletion UI. Its stand and evidence remain protected.

The orchestration case `qa.orchestration.utc-readiness` preserves the C7 polling correction in `qa.local/architecture-stage-c-repair7-independent-functional/operator-response.md` and `session.log`: compare normalized UTC timestamps and inspect actual response content before concluding a wait failed. Observation delay does not justify duplicate input or restart. This is helper evidence, not a product PASS.
## Lossless archived evidence

Historical reports keep their original path spelling. When an original path is
absent, resolve it through `archive-manifest.json` in the local cleanup directory.
Decompress its gzip artifact and verify `originalSHA256` before inspecting it.
No frozen report, source manifest or source snapshot is rewritten for archival.

| Original path | Archive relative to local cleanup directory |
| --- | --- |
| `qa.local/catalog-orders-functional/provider.jsonl` | `archives/catalog-orders-functional/provider.jsonl.gz` |
| `qa.local/catalog-orders-functional/history-observed-chunks.jsonl` | `archives/catalog-orders-functional/history-observed-chunks.jsonl.gz` |

Both complete logs were restored and hash-verified before raw-file deletion.
9,234,731 raw bytes are retained as 562,848 gzip bytes, saving 8,671,883 bytes.
The stand's original report records completed retirement; the complete report
was manually reconciled in batch 02. All raw information remains recoverable.

## Retention rules

Before removing an evidence tree, preserve each unique expected outcome and its
locale/input/permission/retry/restart dimensions in neutral stable cases. Preserve
candidate/fixture/request attribution and the old verdict, including failed
attempts and skipped checks, in the local register. Keep authoritative state and
boundary observations when the summary cannot independently support the claim.
Then check all references and active consumers. Age or a newer directory name
does not establish supersession.

Frozen source snapshots, manifests, import baselines/deltas and stand configuration
dependencies are retained even when their prose duplicates another file. Protect
all `architecture-stage-c-repair6*`, `architecture-stage-c-repair7*`,
`architecture-stage-d-final-composition`, `architecture-stage-d-bot-lint-repair`,
`architecture-stage-d-registration-retention` and
`architecture-stage-e-final-integration` trees and dependencies during current work.
Keep shared and active Go/pkg/build caches while builds or lint run. No global
Docker prune or unrelated container/database operation belongs to this cleanup.

## Completed cache cleanup

All three active builders confirmed no use or planned use of these six caches. Current-reference checks found only historical gate notes. Before deletion each exact absolute path was checked inside the repository, all descendants were checked for reparse points, file names were verified as reproducible Go/lint cache entries, and the latest file timestamp was before 2026-09-27. [cache-cleanup-manifest.json](../qa.local/qa-evidence-cleanup-20260929/cache-cleanup-manifest.json) records exact paths, counts, sizes, timestamps and confirmed deletion.

| Removed cache | Bytes |
| --- | ---: |
| `qa.local/events23-lint-cache` | 7,583,405 |
| `qa.local/migration-lint-cache` | 902,139 |
| `qa.local/sqlc-lint-cache` | 1,248,856 |
| `platform/.cache-diagnostics` | 645,643,754 |
| `platform/.gocache-model` | 579,085,215 |
| `platform/.lintcache-model` | 3,333,570 |

Cache deletion removed **1,237,796,939 bytes** in 47,094 files. Together with lossless log compression, net file bytes freed are **1,246,468,822** (about 1.25 GB / 1.16 GiB). This is measured file length, not a claim about filesystem allocation or total drive free space. The separate 102-container cleanup is not included.

Protected caches include `platform/.go-cache`, `platform/tools.local/lint-cache`, the C7 race Linux Go cache, `architecture-stage-b-repair6/timeout-probe/go-mod-cache`, and the planned delivery-boundary repair lint cache. Python bytecode candidates remain because running Python consumers and baseline/import helper dependencies could not be excluded. Source snapshots, old binaries, stand configurations, attachments and baseline/import trees remain where reference or consumer uncertainty exists. No broad evidence-tree deletion or Docker prune was performed.

Written by Codex (GPT-6/Codex)
on behalf of Daniel Drizhuk
