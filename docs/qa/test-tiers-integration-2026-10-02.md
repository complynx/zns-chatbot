# Test-tier integration

Integrated exact candidate `20b5c8a0dd2d2aa7c9a0dccefe82401e6a9dedb4`
by fast-forward from `eb8fd236c218f5a433cb625cfb7a1036bfb9d5ab`.
Only the five reviewed test-tier files changed. Product source was unchanged.

Independent [Code QA338](code-qa-2026-10-02-338.md) accepted the source,
including QA-authored wait classification and dependency mapping.
Separate [Functional CLI QA](functional-test-tiers-2026-10-02.md) accepted
the public tooling behavior. Its earlier probe incidents remain disclosed.

Affected Linux integration checks on the exact committed snapshot passed:

- Pinned ESLint and Prettier.
- All 12 Node tests; no failures or skips.
- Native Go discovery: 65 runnable packages, 2381 selectors partitioned into
  2300 fast and 81 slow, without overlap, omission or extras.
- Complete tracked-source hashes and clean Linux Git status before and after.

Controller `b172816a1c6b` finished exit 0 without OOM on 2 October 2026,
17:37:34-17:39:15 UTC. Slot B used at most 2 CPUs and 4 GiB, network none;
actual mounts and resource configuration were checked before start.
Source, dependencies, tools and seed were read-only; only owned cache and
results were writable. Receipts remain in ignored
`qa.local/qa-wait-inventory-337/integration-receipts/`.

No product test bodies, database scenarios or full acceptance gates ran in
these integration checks. Discovery can execute package initialization and
TestMain. No execution-speed improvement is established. Existing full/default
and final gates, race settings, scenario budgets and independent product QA
remain mandatory. Go-platform Functional40 is not accepted by this tooling.

Usage and the intentional wait inventory: [test tiers](../test-tiers.md).
Classification ownership and execution policy:
[coordination process](../coordination-process.md).

Written by root (gpt-6.1-sol/Codex)
on behalf of Daniel Drizhuk
