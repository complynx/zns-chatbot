# Full Dockerfile image QA — 2026-10-15

The unmodified `deploy/shuttle/Dockerfile` now has a complete local build,
byte-level contents check and isolated runtime acceptance. This closes the
packaging gap left by earlier candidate images that reused files from an accepted
base. No production source, schema, dependency or Dockerfile changed in this slice.

The local image ID is
`sha256:06d7ecf49171cd5780e7edc13c6e885d1b430460008b66545a61bbcc7d315d51`.
It is 57,334,599 bytes, Linux/amd64, with user `10001:10001`, SIGTERM, the expected
Shuttle entrypoint and config argument. It is a local QA artifact, not a published
registry digest or an approved release commit. Production rollout remains gated.

## Build and contents

Buildx used a dedicated pinned BuildKit container, 1,536 MiB memory with the same
memory/swap ceiling, a two-CPU quota and one concurrent build vertex. Native
HostConfig checks verified those limits. The build completed with exit 0 and no
observed builder OOM. The shared selected builder was unchanged. Only the owned
builder and its temporary cache were removed afterward. Resource configuration
follows Docker's [container driver](https://docs.docker.com/build/builders/drivers/docker-container/)
and [BuildKit parallelism](https://docs.docker.com/build/buildkit/configure/#max-parallelism)
contracts.

The actual full Dockerfile downloaded and verified Go modules, compiled both
executables and copied its final scratch image contents. A 661-file inventory
covers the Dockerfile/ignore, module files, source/assets/migrations, project
license and both notice directories. Hashes matched before and after build,
inspection, runtime execution and cleanup.

The image export contained all 393 expected repository license/notice files with
exact matching hashes, including the project license. Go LICENSE/PATENTS and the
CA bundle matched the pinned Go build image byte for byte. The bundle also parsed
as a nonempty trusted-CA set. Both binaries were executable ELF files. Source
trees, Go build caches, startup configuration, shell and curl were absent from
the inspected paths.

The main binary SHA-256 was
`2b6955539084b52be5f2ee027bcba47bb54fd3eda922507790d6e02c9a5726fc`,
identical to the previously accepted historical-discovery executable. The avatar
binary SHA-256 was
`ac0638e2ed2eb2f7b07bf98a8d37c2cdabfe467a480230470ef48cea7c65503b`.
With no network, a read-only filesystem and a bounded non-root container, it
rendered a synthetic PNG to a valid 1024×1024 JPEG with correct source/result/frame
hashes and processor version. Independent JPEG decoding and visual inspection
confirmed the existing frame and synthetic blue input. Invalid image bytes
returned exit 2, empty stdout and the expected sanitized error. Probe containers
were removed after terminal states and output were saved.

An earlier suspected Docker-ignore exclusion was disproved by a native minimal
COPY and by this full build. Exact avatar notice bytes were present. That earlier
probe's observer initially compared Docker-injected metadata with license files;
the corrected subtree comparison reused the same saved export. Its failed
observation remains separate evidence. No speculative allowlist patch was made.

## Runtime

The full image ran against a fresh owned PostgreSQL, received the normal Goose
and River migrations and exposed schema 32 / manifest `mvp-32`. Only existing
owned user rows were copied to support real local Zitadel authorization without
creating an identity. Tasks, conversations, outbox, notifications and history
were not copied. Telegram used a dummy token and loopback counting/rejecting sink.
Loom used a narrow local protocol fixture; the real Loom stand was unchanged.

One local webhook update created one durable task. The fixture issued one auth
session/run and held a read only after their IDs were persisted. Actual SIGTERM
cancelled that request and exited 0 without OOM; its conservative observed
process-exit upper bound was **242.9726 ms**, including Docker command/wait overhead.
Drain was **7.847778 ms**, with every participant and aggregate completed.

The same container/image restarted while the fixture remained blocked, read the
same run with the same session and then persisted a released running-version-2
observation. The second actual SIGTERM exited 0 without OOM within a conservative
**234.169 ms** bound, with drain **7.905829 ms** and its own complete summary.
No Docker stop fallback or application rescue kill was used for these measurements.

After that final drain, the task/run/session identities and snapshot were intact;
the same River job was available for future processing. Final counts were one
task, one inbox, zero outbox, one session bootstrap, one run create, three reads,
two successful responses and one cancelled read. Telegram and unexpected-route
counts were zero. This is continuation of unfinished work, not task completion
or Telegram delivery.

All original stand `shuttle_%` table fingerprints, bot/Loom container identities
and start times, and configuration hashes remained unchanged. The original stand
still uses its prior accepted image. All new runtime containers and the temporary
PostgreSQL volume were removed. The fixture peer's later cleanup exit 137 without
OOM is separate from application exit-0 evidence.

## Review and boundaries

Four fresh review rounds fixed substantive observer gaps before native acceptance:
matching inspection prerequisites, failure/output/state persistence, webhook
dispatch receipts, final counter and recoverable-job checks, complete source
inventory rechecks and a bounded migration invocation. The final source review
found no actionable findings. Runtime failures invalidate the checkpoint and do
not trigger blind task re-admission. Reviewed helper hashes matched after QA.

The existing source-matched 259-scenario race inventory remains applicable; it
was not rerun merely for this packaging check. Its retained notification timeout
and diagnostic/recheck passes remain in the
[discovery QA report](shuttle-history-discovery-qa-2026-10-15.md).

This acceptance does not establish production deployment, unattended inference,
real Loom/Zitadel outage recovery, worst-case shutdown, child-process termination,
browser linking or a new vulnerability audit. Those gates and earlier failures
remain open. The observed shutdown is a context-cancellable HTTP case. No commit,
push, registry publication, production or Cloud change was made.

Ignored build/inspection evidence is `.cache/shuttle-work/full-image-qa`; runtime
receipts and stop logs are `.cache/shuttle-live-test/full-image-runtime-qa`.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
