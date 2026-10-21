# Local candidate installation acceptance

On 2026-10-21 UTC, the owned `shuttle-live-test` stand accepted application source `4f03e5ff7a0c8db0f322ebd7f22cdb275ac30b7b`, schema70 / manifest `mvp-91`, and published Loom revision `34f4646832748efd455828f6804c6274027db659` (application schema4). The original Telegram poller resumed; all11 services are running. This is a local test installation, not production deployment.

## Candidate and prior evidence

The bot image is `sha256:a9c2d76e5f3a01889ef041e4bfba8131433a84241829e6cbd1656815e00869fd`. Loom API and sandbox/run worker both use `sha256:003529b117602d52905e1d5f65d353fbe45e969dfc065eae638627b379f813ff`. File processing remains on the previously accepted separate worker image.

Application source passed [CI48](https://github.com/complynx/zns-chatbot/actions/runs/39678246176):877 PostgreSQL/race integration roots,370 UI tests, generated SQL, zero lint issues and Linux/container builds. [Isolated actual-IAM operation adoption](shuttle-loom-adoption-acceptance-2026-10-21.md) passed native/agent continuation, concurrency, receipts, restarts and rejection. [Clone migration and rollback](shuttle-candidate-migration-qa-2026-10-22.md) passed original-data preservation and restoration before this installation.

## Executed installation

The reviewed controller stopped the original poller, drained accepted work, then stopped bot, Loom API and sandbox. All four exited0 without OOM. The bot stop command completed in0.687 seconds. Fresh custom archives of both application and Loom databases were fully decoded; final logs of the old processes were retained before replacement.

Loom migration and both runtime replacements preceded the bot migration/capability. One configuration guard then stopped the procedure: the effective Compose inputs selected the base `loom.yaml`, whereas the actual accepted Loom container used the counted test-provider config. The full original settings hash was recovered from a retained snapshot; replacing only that bind source reproduced it exactly. The original config contents were already pinned in accepted inputs.

A separately reviewed recovery retained the original failure, restored that exact config binding in forward and rollback Compose inputs, and verified that all other settings and original rollback images matched. Its first preflight stopped before runtime changes because Compose retained explicit empty bind options; the corrected preflight preserved those options. Recovery pinned the known partial-upgrade IDs, replaced only the misconfigured new API process, and continued only the bot migration/replacement steps that had not yet been requested. It did not repeat the completed Loom migration or worker upgrade, or any business action. Ingress remained fenced throughout.

The final read-only gate checked all18 installed Mini App assets against source digests and privacy/security headers, readiness, the `zinusya` / `mvp-91` manifest, inspector capability header, and401 for unauthenticated inspection. Startup configuration hashes matched the original baseline for every service. The original poller was started once; its identity and successful polling cycle were checked.

## Preservation and limits

The comparison preserves all original application and Loom durable rows, operation documents and sequences. Application migration metadata advances69 ->70; the new avatar expiry marker is NULL. River leader leases may change with restart, queue heartbeat timestamps are excluded, and only already expired terminal River jobs may disappear. These operational allowances do not cover business or authorization data. The old unknown pair assignment and prepared order-payment review still belong to cancelled runs and were neither reconciled nor resent.

Protected companion containers, identity, configuration and existing file references remain preserved. The new state has its own accepted runtime epoch; the previous schema69 observer remains historical evidence and must not be used as the current guard.

This installation gate does not repeat authenticated business workflows or prove autonomous model quality. New-runtime rights-revocation/restoration and abrupt-recovery acceptance, durable asynchronous trace context, joint CA/ZitadelTG/real-Telegram WebView testing, and production export/migration/cutover remain separate work. After ingress resumes, restoring the pre-installation database copies is not automatic; reconcile new work and plan recovery explicitly.

Private installation evidence, original failed observations, exact IDs, decoded archives, logs and the accepted epoch remain under `.cache/shuttle-work/primary-candidate-install-20261022-v1/installation/`. The independently reviewed controllers are stored beside it and remain ignored because the evidence references local configuration and credentials. Current read-only guard: `install.py:observe()` in that parent directory. No Loom developer checkout/stand or production service was changed.
