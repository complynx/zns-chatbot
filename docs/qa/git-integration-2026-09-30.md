# Git transition and first integrated correction

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
