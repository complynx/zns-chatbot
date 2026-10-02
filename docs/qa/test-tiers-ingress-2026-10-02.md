# Ingress test classification acceptance

Candidate `5e4cb82a4a01d58b8380af10fffb71e156ff4ee5`, base
`0c4411d42ad9c93c8155a922a162363d9b6e0aef`, has exactly three paths:
`docs/test-tiers.md`, `platform/scripts/slow-tests.json` and
`platform/scripts/test-tiers.test.mjs`. Independent source-only Code QA356
passed. The candidate was integrated by a serialized local fast-forward.

QA classified the durable original/replay and captured-custody expiry ingress
scenarios as slow because they exercise actual waits. Short unit and owner
diagnostic cases remain fast. The synthetic discovery fixture contains the
two new scenarios. Full/default/final gates, pins and budgets are unchanged.

## Executable proof

The complete author Linux gate passed in 441.61 seconds under its original
600-second limit: pinned lint/format, 12 Node tests, actual slow/fast/changed
plans and disjoint partition/membership checks. Inventory is 83 slow and
2,313 fast; changed selection against the exact base contains all 2,396.
Both new scenarios are slow and changed, not fast.

The separate actual-root affected gate passed in 496.21/600 seconds,
controller `f544136c775be17368580c8462e4c668da32d4c5ec8b2490e7ab3bb6cfe6bb5a`,
terminal exit 0 and no OOM at 2026-10-02T23:41:38.375596294Z. It repeated all
checks together; no earlier partial gate contributed to its PASS.

Both used the existing qualified Linux toolchain, 2 CPU/4 GiB/equal swap,
network none, read-only root/source/Git/dependencies and separate owned writable
cache/evidence. Actual CREATE guards preceded START. Source I/O was addressed
by byte-identical native Linux copies, not changed commands or larger budgets.

Actual root had 2,993 tracked files: 2,801 Git-byte-equal, 191 declared
newline-only differences and the sole pre-existing user change in
`docs/code-quality.md`. All original/copy/before/after raw guards passed.
The user file remained SHA256
`306e99e1cbad613ca8f54784f724be4e6f0397a91660c58735dccdee614d6437`.
This is an independent actual-root baseline, not an author MATCH claim.

## Limits

This accepts the classification change and affected integration, not C/E
runtime, full product test bodies, standalone Functional behavior, a general
speedup or production readiness. Earlier timeout/interruption/preparation
failures remain separate failures. Operational receipts stay in ignored
`qa.local/`; current scheduling and estimates stay in `management.local/`.

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
