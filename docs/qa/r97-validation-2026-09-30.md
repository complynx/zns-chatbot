# R97 receipt error propagation

Scope: original registration-admission SQL reads, retired receipt transaction Begin/Commit, and the interaction reader's error precedence. This is a focused repair, not full AUD23, migration or Functional acceptance.

## Reproduction and candidate proof

The unit overlay adds actual pgxpool connection failures with EOF and driver-local cancellation/deadline, genuine parent cancellation, and consuming interaction tests. The consumer cases place a successful summary before a joined SQL/403 or SQL/404 error and verify no partial result or subsequent domain call. Ordinary denial still permits other authorized entries.

- `r97-unit-red-1`: exit 1, 11 FAIL and 8 PASS events against unchanged product code.
- `r97-unit-green-1`: exit 0, 19 PASS and no FAIL/SKIP with the external candidates.

The PostgreSQL overlay uses real stored admission JSON, including absent/null/empty sources, invalid typed values and no partial results. Retired receipt cases inject actual pool Begin failure or a query-local Commit deadline. They check a live caller, safe database error, empty result, unchanged canonical receipt, recovery without replay, missing/foreign witness refusal, actual parent cancellation and current permission withdrawal.

- `r97-pg-red-1`: exit 1, 5 FAIL and 9 PASS events against unchanged product code.
- `r97-pg-green-1`: exit 0, 14 PASS and no FAIL/SKIP with the external candidates.

Counts include parent and subtest events; they are not counts of defects. All logs and overlays are under `qa.local/go-resume-20260929/`. Failed runs are preserved. No production or persistent functional stand was changed.

## Review and application

Code QA166 is clean for the interaction source and its new tests. The two files were hash-guarded into the working tree. Pinned interaction lint (`r97-mixed-lint-1`) exited 0 with zero issues.

Code QA167 is clean for the separate pinned four-file admissions/retired-receipt candidate. Root applied it after checking all original and candidate hashes. The pinned formatter changed only integration-test layout. The combined final working-tree run `r97-applied-1` exited 0 with 33 PASS and no FAIL/SKIP events across the three affected test packages. The first combined lint found one staticcheck suggestion in the test: replace an if/else-if with a tagged switch. Root made that mechanical edit; QA167 independently confirmed full-diff equivalence and the final hash. The repeated pinned lint `r97-lint-2` exited 0 with zero issues. The changed retired-receipt test family also passed its five events in `r97-runtime-1`; the separate new runtime proof in that run failed and is not accepted.

## Remaining proof

The helper and coordinator checks alone do not prove owning-runtime behavior. The additional reviewed runtime test below establishes the selected admissions-failure route, cancellation effect fence and FIFO preservation. R98 has repaired and checked the separately scoped transitive receipt helpers. Full isolated-worker and independent Telegram-like Functional acceptance, complete composition and other effect families remain open. Immediate invitation presentation remains R96 design work. Pending R56 authority changes are excluded.

Written by Codex (gpt-6-astra/Codex)
on behalf of Daniel Drizhuk

## Runtime proof iterations and final result

The initial external owning-runtime proof did not pass. Revision1 reached the fatal SQL exit but counted three pre-fault startup API controls as unexpected delivery. Revision2 recorded their exact identities and timing (`setChatMenuButton`, two `setMyCommands`, then `getUpdates`, all before failure); it failed a chat snapshot taken before the test's own user input. Revision3 preserved exact input and chat checks and reached the final inbox assertion, which used a guessed update ID. The sandbox shares its counter between sent messages and input updates. A fresh developer is binding the assertion to the actual input response IDs. These are test-fixture findings; no runtime product change is justified by them. All failed runs remain available.
Runtime revision4 (`r97-runtime-4`) exited0 and proved the exact lab response IDs4/5 remained pending with zero failures. Fresh QA169 then identified a coverage gap: counting successful later callbacks does not exclude an attempted effect returning an error. Revision5 adds a forwarding Host.HTTP spy calibrated by the real seeded invitation's exact command endpoint. It requires zero post-fault effect requests regardless of response. The real Sobek VM stops on cancellation; a separately identified late-executor probe invokes the cancel callback once without the canceled context and requires the SQL fence without dispatch. `r97-runtime-5` exited0. The complete final test is in fresh QA170; no product change was needed for this evidence repair. It remains an external test until reviewed and applied.
Final runtime candidate passed fresh independent Code QA170 with no blocking findings. Root hash-guarded the new test into the working tree. `r97-runtime-final-1` exited0, 1 PASS and no FAIL/SKIP. It confirms the specified owning-runtime failure, cancellation effect transport fence, exact pending FIFO and user-visible output boundaries. This is an actual in-process Sobek flow plus a separately labelled adversarial late callback. It is not isolated-worker-process, supervisor-restart, raw callback-buffer serialization, arbitrary effect-family, or independent UI Functional proof. The first final integration lint found only a missing blank line separating an embedded test field. Root added that line; QA170 verified the entire one-line diff and bound final SHA256 `E8CA9A301F542B951241634350CF667BF556A844144881F13F55038E080440B6`. Runtime proof belongs to the identical executable code before that whitespace-only edit. Repeated final integration lint `r97-runtime-lint-2` exited0 with zero issues. All seven final R97 files match their recorded reviewed hashes. Both root runtime and lint jobs are terminal.