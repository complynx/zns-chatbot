# Functional tooling QA completion delta

Candidate: `20b5c8a0dd2d2aa7c9a0dccefe82401e6a9dedb4`.
Final verdict: **PASS for the requested test-tier CLI tooling scope.**
The earlier partial `REPORT.md` and all failed probes remain preserved. This
delta closes its three coverage gaps; it does not accept migration product
tests, wait classification, speed gains, Go-platform Functional40, or full gates.

## Closed cells

1. Rename new-side selection: an independent synthetic Git repository renamed a
   known pass path into the known order path. Its plan selected the same set as
   the independently tested order edit: 48 slow selectors, compared with 41 for
   the old pass path. This proves new-side order dependencies were added. The
   earlier order-to-pass case proved old-side retention. An additional unknown
   old docs path renamed into orders selected all 81 slow selectors, preserving
   conservative handling of the old unknown path.
2. External Go exclusion: an opaque owned clone of the exact candidate received
   a synthetic runnable Go test package under `platform/node_modules/fqa-external`.
   Native Go listing found that package. The actual public fast plan excluded it,
   retained exactly the original 2300 fast selectors, and planned no node_modules
   package. The synthetic package test body was not executed.
3. Mandatory migration baseline and child failure: an owned Go runtime clone
   replaced only its own public `bin/go` entrypoint. All four tiers reached
   `go -C ../tools/migrate test -race -count=1 -p 2 -parallel 4 ./...` exactly once.
   The wrapper returned controlled status 23 without running test bodies; each
   public CLI returned 1 with `Migration tests failed: 23`. Fast/slow used the
   public direct entrypoint; all/changed used the public npm commands. This proves
   every-tier runnable baseline dispatch and fail-closed child propagation. It
   does not claim the baseline test bodies pass.

The existing independent native inventory found 137 runnable secondary-module
names; the dispatched `./...` baseline includes its main and command packages.
Primary partition evidence remains 2300 fast + 81 slow = 2381 native runnable
names, with no overlap, omission, or extra selection.

## Runtime custody and terminal proof

Preparation `9b8601fa049a` copied protected source/common Go opaquely into separate
owned volumes. No implementation or manifest was opened. In executable probes,
protected source, synthetic source clone, owned Go runtime, dependencies, and
control aliases were read-only. Only owned cache/output were writable. Actual
Mounts and HostConfig were inspected before each start: 2 CPU, 4 GiB, network
none, neutral targets, no writable protected ancestor.

The final argument-aware wrapper SHA256 was
`b596164448ee9ccf3c9985f5c889d0160ec0a19fc4dd46e48b692def2039569f`.
PATH and absolute public Go paths, including global `-C` flags, passed synthetic
zero-body guards before the successful baseline probes. Unknown non-discovery
Go commands fail closed in the wrapper.

Final controller
`e8fe5fa2e88e6848140f1068d3167f8f44287f15ec8a737cf7b919fe3f5d037c`
finished `2026-10-02T17:24:22.726661423Z`, exit 0, OOMKilled false. Final original
source HEAD/status guard passed, and all 2980 tracked hashes passed again.
Heavy slot B was explicitly released to E before report-only synthesis.
`final-evidence/final-analysis.json` records exact comparisons and intercepted
commands; complete command outputs are retained beside it.

## Preserved incidents

The first wrapper recognized only a first argument `test`. The actual migration
command begins with global `-C`, so both the initial PATH shim and first owned
runtime wrapper missed it. Two earlier probes therefore ran real migration test
bodies against unreachable fake `.invalid` database URLs. Both failed; network
was none, no database was reachable, and no database mutation occurred. The
successor stopped at its first missing interception marker (controller
`98a3be25ff95`, exit 91/OOM false); failed output is preserved in
`successor-evidence/migration_fast.log` and the original evidence directory.
This incident is not counted as product acceptance. Corrected probes intercepted
the global-flag test command before executing any body.

A later owned harness tried direct mode `all`, whereas the public runner uses
`full` behind npm `test:all`. It stopped with the expected invalid-mode error
(`2ad804ed353f`, exit 91/OOM false). The final successor ran only the remaining
public npm all/changed cases. Report-only synthesis also corrected one literal
command-pattern assertion; no product gates were repeated for that correction.

Written by functional_qa_tiers_20b5 (model-unexposed/Codex)
on behalf of Daniel Drizhuk
