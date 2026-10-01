# Go source provenance — 2026-09-29 resumption

## Current integration — 2026-10-01

Authoritative product source is Git0070f944691050677cd915a215e4ac0beea69fb6.
Reviewed callback observations62b9eebc were merged6812e359; source diagnostics
b857b8b9 were mergedf56bebd3; registration fixtures54766266 were merged0070f944.
Each merge preserved the independently reviewed candidate; no product conflict
resolution or additional product edits occurred during these three merges.
The working root documentation is updated separately. Functional images remain
sealedce427ca5, so they do not certify this newer source. Menu090, job summary
and E rehearsal guard successors remain unmerged.

Migration checksums bind raw embedded bytes. Equal Git blobs in CRLF and LF
worktrees do not prove equal embedded migration hashes. Final importer/runtime
images must share a verified raw Git export and actual migration inventory;
existing ledger checksums and applied_at are never rewritten for convenience.

The composition and inputs below are historical provenance, not a fresh
acceptance of current source or a reason to restart development from old trees.

The authoritative working source is `platform/`, with the importer in `tools/migrate`. Preservation commit: `1ed6f8fb07698f29314536d98ba44f04f026a7ab`. Composed commit: `b99c4822ba9110dde5009e0aac738797cbb5ae66`; all current `platform/` source is tracked. `git diff --check` and a credential-pattern scan of 1104 files reported no findings. Historical QA trees were intentionally not copied into Git; required evidence portability remains separate. No new source snapshot trees were created; subsequent evidence binds to Git commits and exact reviewed files.

The paths below identify original local handover inputs. `qa.local` is ignored by Git; these bindings preserve provenance, not a promise that the local trees exist in a fresh checkout. New development must not restart from those older trees. Previous source-specific checks do not accept the composed source.

## Original inputs

| Input | Source path | Binding |
| --- | --- | --- |
| C7/D fixture baseline | `qa.local/architecture-stage-d-c7-fixture-repair/source` | Baseline manifest SHA256 `8F6F02869FB78D9491509C6CD500AC776412AFD69E73EB7C47F60D116B794C7A` recorded by repair handoffs; 1780 files |
| C7/D + intake | `qa.local/architecture-stage-cd-composed-acceptance/source` | 1805 files |
| Delivery | `qa.local/architecture-stage-d-delivery-boundary-repair/source` | 75 delta entries; 1800 source files |
| Memory retirement | `qa.local/architecture-stage-d-script-retirement-repair/source` | 4 delta entries; final-delta supersedes old delta |
| Modern order reads | `qa.local/architecture-stage-d-modern-read-repair/source` | 4 delta entries |
| Asynchronous fixtures | `qa.local/architecture-stage-d-async-fixture-repair/source` | 4 test-only delta entries |

Intake was already applied to the 1805-file input. Its 56-path handoff is recorded below for provenance and must not be applied a second time.

## Manifest and delta SHA256

| Path | SHA256 |
| --- | --- |
| `qa.local/architecture-stage-cd-composed-acceptance/handover-source-manifest.json` | `D64D51CC89FE6BF29B7BE391AEA76B247B00A4AD91BF033C5F51E9A2358E7E4A` |
| `qa.local/architecture-stage-d-registration-retention/intake-successor/handoff/changed-files.json` | `0F3AE995367F27476E9BC5F5B2EC8915AE13984793B36AB3C5674DFD76A405B4` |
| `qa.local/architecture-stage-d-registration-retention/intake-successor/handoff/final-manifest.json` | `2B7DFB17A9E89ACFD508EC07A82DD2F9120A67A994C780ED0CE3C687BFB5647D` |
| `qa.local/architecture-stage-d-delivery-boundary-repair/checkpoint-manifest.json` | `2E1B0FB39C58C46B9D4C1E36BA0E87D79099E2AE598856239695DD01196110D1` |
| `qa.local/architecture-stage-d-delivery-boundary-repair/checkpoint-delta.json` | `56F25BD7FE4926B661154BB74A89FE5A25F928F0F7C5002BD47AF223DCD6029D` |
| `qa.local/architecture-stage-d-script-retirement-repair/final-manifest.json` | `C0FF00BB7E6B8F61F0758A5502D80D7868121F374376F301525B9C842759CA30` |
| `qa.local/architecture-stage-d-script-retirement-repair/final-delta.json` | `7A4812FFAA51FD60C29E4A5A46ACDEE2D26CE6148922C0A86FA279ED15B0E59C` |
| `qa.local/architecture-stage-d-modern-read-repair/native-after.json` | `51FEEA85779E7EAE6FF3A7409CFD191A0B9BDDAE89D337E208746D4C52F94359` |
| `qa.local/architecture-stage-d-modern-read-repair/delta.json` | `822855A0BD56A3718EE9B1C52135EAC2741E0F9E65891FE58507B8BBD13E419C` |
| `qa.local/architecture-stage-d-async-fixture-repair/source-manifest.json` | `E98844E21E334D301CE80E686B3A497F1B679333224777EFFD22B67F68C9B780` |
| `qa.local/architecture-stage-d-async-fixture-repair/delta.json` | `1DDD08CB98BF4F9053B8E662D6BB58A69D106449C9EDC0BCC3A4B7F528D8F8F6` |
| `qa.local/go-resume-20260929/composition.json` | `7FCF832D15055A89B4BF58676C2E3CE2C9E16A83F2B7F943BADEE2111B1D5589` |
## Composition record

`qa.local/go-resume-20260929/composition.json` records 1830 composed candidate files, five merged paths, four explicit delta deletions, 45 obsolete source removals and 11 retained auxiliary files. Counts are taken from that report's arrays. Earlier progress narration used 46/10; the recorded arrays are authoritative. Removed older source is preserved by the preservation commit.

Merged paths relative to `platform/`:

- `cmd/zns/app.go`
- `integration/notification_delivery_fixture_test.go`
- `internal/appservices/services.go`
- `sqlc.yaml`
- `integration/script_knowledge_privacy_test.go`

The first four combine delivery and intake changes. The privacy test combines separate function changes. The SQLC block was appended manually; other overlaps used three-way merge against the fixture baseline. The report records composition, not acceptance, and does not hash subsequent fixes or formatting.

## Retained assets

Keep `platform/internal/miniapp/foodphotos` JPGs (approximately 11.48 MB): production embeds use them. Keep synthetic `platform/testdata/media` (approximately 425 KB): fake ASR uses their hashes. History and detailed documentation remain intact; the old PROGRESS content is preserved in `docs/handover-status-2026-09-29.md`, with trailing blank lines normalized. Its exact original bytes remain in the preservation commit. These are intentional source assets, not disposable build caches.

## Verification boundary

`qa.local/go-resume-20260929/compile-2.log`: full compile of the five authored roots (`cmd`, `internal`, `integration`, `identityprovision`, `deploy`) passed, exit 0. `importer-compile.log`: both `tools/migrate` packages compiled, exit 0. Both runs are compile-only; no tests ran. The initial `compile.log` remains historical failure evidence.

Gofmt and pinned formatter 2 completed; pinned formatter exit 0. Focused PostgreSQL composition checks passed all five top-level scenarios, no skips, 8.187s: delivery split-role/restart, delivery revocation, native retention FIFO, external memo deletion before the next plan, and memo-delete list retention. This is developer proof, not Functional QA.

Native/PostgreSQL checks of all authored roots (session 53470) were INCOMPLETE after a panic, exit 1 against composed commit `b99c4822ba9110dde5009e0aac738797cbb5ae66`: 2237 pass events, 104 failed test events and 26 skips; integration 477.001s, with 877 started tests missing terminal events. See the baseline completeness addendum; this is not a completed suite. SQLC regeneration diff also failed, exit 1. Details and limits: [composition baseline](qa/composition-baseline-2026-09-29.md). Strict pinned lint (`lint-all-1`, session 39117) exited 1 with 47 issues: gocognit 3, goconst 40, govet 1, mnd 1, nestif 1, whitespace 1. No exemptions were added. Current task status and later gates are tracked in `PROGRESS.html`.

The subsequent native/PG run 3 completed with exit 1: 3250 pass / 294 fail / 28 skip test events, zero panics and zero started tests without terminal events; only integration failed. Its working-source manifest covers 1980 files, with zero source or inventory drift. This later source is bound by `qa.local/go-resume-20260929/native-pg-all-3-source.json`, not by assuming the original composition commit contains subsequent repairs. Full lint 5 has 23 issues. Freeze was lifted for the assigned repair batch; see the baseline report terminal addendum and PROGRESS.html.
