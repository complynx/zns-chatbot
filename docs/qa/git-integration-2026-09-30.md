# Git transition and first integrated correction

## Reviewed next batch — 1 October

Prepared developer branches and independent static reviews merged without content
conflicts:knowledge5f3ca030→cb65243f(QA228 addendum), managed fixtureecfbd188→1c485ddd
(QA229 addendum), privileged-read7e8dacee→5a4d2f27(QA230). Developers owned base
preparation, conflict resolution and local gates.

One combined root native command on5a4d2f27 covered complete cmd/zns,config,
sandbox,agenthost,observability,identity packages:exit0,678 test/subtest PASS,
0FAIL,5SKIP; six package outcomes PASS. TEST_DATABASE_URL was empty for this
unit/config batch. Skips:CombinedClientRunsRegistrationBatchPostgres,
RuntimeServerLossCancelsAcceptedRequest/running and/draining,
DeliverySurvivesRestart,ZitadelLocalAdapter. Unexecuted is not passed.
Pinned affected lint including bot:exit0,0issues. Terminal logs retained under
platform/qa.local/git-composition-20261001-tests.jsonl and-lint.log.

Then modern-order73687953(QA232) and prepared stand seam146e17cc(QA231) merged;
ce427ca56ff9f127eba55321047c64a5e9204683 is the immutable build source epoch.
The678 result predates these two merges; it is not current full-composition proof.
Engineer executes new-epoch checks and actual builds. Image/container provenance,
SQL contention, managed replacement and independent EN/RU UI FQA remain pending.
Three templates do not establish three ready stands.

FQA lead manages flow/recovery/import stands and two fresh source-blind reviewers.
Both plans have40 unexecuted scenarios. Kanban manager alone writes observed
coordination-metrics.csv; historical task starts stay unknown. Test counts are not
readiness percentages. Local build/preparation progress does not accept a stage.

## Historical initial checkpoint

Daniel authorized consolidating current work into the existing branch and switching
development to Git. Checkpoint ce994271 preserves960 changed source/docs files;
it is not a new acceptance verdict. No Python changes or local caches were added.
Local lint cache files were removed from the index only and preserved on disk.

Each developer now owns a separate task branch/worktree. Local gates run there,
reviews bind to commits, merges serialize into feature/go-platform-sandbox.
KANBAN.html records task ownership and handoffs. No push or production action.

First source merge:75a65e21 from codex/host-cache-quality commit
d9d00ca96e42bb8e7e3a52826bf49def94b849b7. Root independently hashed all nine
Git source blobs against frozen QA224 final hashes; every blob matches. The tenth
file is local gate evidence. No merge conflict or additional source change.

Developer gates: pinned formatting, affected lint0issues and14 top-level tests
(43 test/subtest PASS events across two commands). Root integrated command:

```
go test -json -count=1 ./cmd/zns ./internal/agenthost ./internal/observability ./internal/identity ./internal/bot -run 'Test(RuntimeAuth|ZitadelSecrets|KnowledgeCatalog|KnowledgePreparation|IdentityCache|KnowledgeTools|CredentialCache)'
```

Root result:exit0,49 test/subtest PASS events,0FAIL/SKIP. Package outcomes excluded.
Pinned affected lint cmd/zns,agenthost,observability,bot:exit0,0issues; existing
exclusion warnings reported0skipped issues. Logs preserved under
platform/qa.local/git-host-cache-integrated-tests.jsonl and
platform/qa.local/git-host-cache-integrated-lint.log. No shared PostgreSQL mutation.

Both root processes were observed to terminal exit0; no retry or duplicate run.
This closes the nine reported quality findings for this composition, not full
Functional QA, current full baseline, architecture completion or release acceptance.

Menu correction has exclusive synthetic database zns_menu_authority_qa, created
only after checking it did not already exist. Shared database zns and container
lifecycle remain root-owned. No production data touched.

## Current integration update — 1 October

Integration HEAD81a9cf73 includes usage c56c9e4f (QA233 staticPASS) and complete model/credit/broadcast catalogs90d84e14 (QA235PASS). Root usage-focused native check completed135PASS/7SKIP/0FAIL across cmd/zns, credits, observability and store without TEST_DATABASE_URL; database-binding, migration-upgrade and runtime-loss cases were skipped. This does not replace developer PG evidence or full baseline. Log: platform/qa.local/git-model-usage-20261001-tests.jsonl. Root affected composition lint completed0 issues after a Windows working-directory access failure; the original failed log is preserved. Actual authorized log: platform/qa.local/git-composition-20261001-usage-host-lint-authorized.log. Complete agenthost/Bot native tests on81a9cf73 completed968PASS/84SKIP/0FAIL (966 case/subtest plus2 package events); skipped PostgreSQL checks are not accepted.

The two real ce427ca5 stands remain frozen and separate from current integration. Both independent source-blind reviewers execute manual/UI subsets; agent capability is independently being checked under its chosen-locale prerequisite. No full Functional PASS exists. QA234 requires an affected SQL-classification correction, assigned to a new developer;1770e5f4 is not merged. Three capability lanes continue registration revalidation, whole-job summary and shared import composition. Full E binds a future reviewed090 epoch, not the historical088 materialization.

Written by root (GPT-6/Codex)
on behalf of Daniel Drizhuk

QA236 passed and shared-composition successor382b778b was merged asf2bcbc5c. Root integrated database-free copied-service and every-outbox-owner tests both passed; log platform/qa.local/git-shared-composition-20261001-tests.log. This supplements developer65PGPASS/0FAIL/0SKIP and does not establish fullE.

Both independent FQA reviewers completed their bounded UI batches and released the ce427ca5 stands with state preserved. Their individual reports and lead combined report explicitly retain blocked obligations across all40 original scenarios; none is fully closed. Root has approved a separate three-file actualcallback-receipt sandbox observation slice, pending development, local gates and freshreview before any newimage.
