# Task closure acceptance, 2026-10-15

This bounded slice exposes existing uncertain-local-task closure through the
agent API. It is local acceptance, not production deployment or readiness.
Settings changes and local user revocation remain operator-only writes.
That boundary describes this checkpoint; the subsequent
[settings acceptance](shuttle-settings-change-qa-2026-10-15.md) records the later
agent settings write.

## Implementation and reviews

Migration 28 adds deployment/actor-scoped proposals, a normalized argument hash,
five-minute database expiry, original preparation and nullable committed result.
Closure reuses the preserving transaction helper used by the operator endpoint.
The agent result commits with the closure and original resolution. Status and
reconcile read only this actor's proposal result with current account/target scope.

Architecture and Go design consultations preceded implementation. Separate fresh
correctness and quality reviews found one contract issue: a byte count rejected
otherwise valid Cyrillic reasons. The store now counts Unicode characters. Two
fresh full-slice reviewers reported no actionable findings after that correction.
The index remained empty; no commit or push formed part of this slice.

## Behavioral verification

The complete enumerated integration suite contained 195 tests, run in 13 bounded
Linux/PostgreSQL batches with the race detector. All passed, as did explicit
`cmd`, `internal` and `migrations` package checks. Strict lint reported zero issues
and `sqlc diff` passed.

After that full gate, only the character-count correction and its behavioral
reason changed in the 233-file source freeze. All task-closure, agent-closure and
existing administrative-read critical paths were rerun successfully under race;
full strict lint again reported zero issues. The updated test prepares, confirms
and recovers a reason containing 512 Cyrillic characters. The entire 195-test
suite was not repeated after this narrow correction.

The new checks cover simultaneous confirmation, original receipt after expiry
and reopening, actor/bot/organization isolation, local revocation, exact proposal
and argument binding, stale versions, live leases, known outcomes, expired
proposals and operator receipt collision. They verify blocked pending/retryable
chunks, preserved unknown/inflight evidence, conditional conversation clearing,
and rollback when result persistence fails. Migration tests verify empty-table
down/up and refusal to discard a retained proposal.

An HTTP test invokes the real handler against PostgreSQL, drops the connection
after commit and observes a client transport error. After restart, status and
reconcile return the original result with one closure/version advance. It does
not infer success from a missing response or create a replacement operation.

Two initial assertion failures are retained locally: time-location metadata in
Go values differed while serialized instants matched; Huma schema links differed
between test-server addresses. Assertions now compare the observable result
without server-address metadata. Neither required a production behavior change.

## Native owned stand

Candidate image:
`sha256:7433abc66d9f6176a9aa723ef716c104d92f008fa21eb1217291a6211178ee5f`.
Previous image:
`sha256:d2a10ad3189dd66ecb1c6a09ec373b77020c98f75a4eaf3e50dd54360e0c2ca8`.
The candidate used pinned Go 1.27.2, a bounded builder and the accepted runtime
base. Compilation passed without OOM. The first packaging attempt treated a
local image ID as a registry name and failed; packaging the same binary from the
verified local base tag then passed. That original failure log remains retained.

A custom-format database dump was taken before migration, hashed and checked
with `pg_restore -l`. Explicit `-migrate` moved schema 27 to 28. The first QA helper
omitted that explicit step; ordinary startup correctly left the schema unchanged.
The original observation was retained and hashed before completing migration.
All 22 selected post-media table hashes and the Mini App origin were preserved.

An existing fixture administrator received a real delegated Zitadel token. Native
Loom discovered `zinusya_administration.close_task`, prepared the exact task,
version and Russian reason, and presented approval. Before approval the synthetic
target was still `delivery_unknown:1`. After approval it was `closed:2`, fence 1,
with one resolution. Pending/retryable chunks became blocked; unknown/inflight
evidence remained at that observation.

Loom run: `35fe11d0-32fd-45c3-b56b-a1331c427121`.
Operation: `08b04f8e-0365-41c1-bba6-bcc7d35c3811`.
The first post-approval QA assertion expected the HTTP envelope in the provider
input. Loom correctly passed only its validated result. The original input and
checkpoint were retained; the same run was finished without another tool call or
approval. Status confirmed the actual committed result.

The previous binary then started successfully with schema 28 retained and read
the closed task through operator diagnostics. Reinstalling the candidate and
calling reconcile returned the original result. This proves the observed binary
rollback/read compatibility, not a schema downgrade with audit deletion.

Proposal and resolution were exported before deleting precisely the synthetic
task/outbox/conversation fixture. All 22 baseline hashes were restored; settings
remained version 1 with IAM cache TTL 0 and the Mini App origin stayed unchanged.
Final observation found nine owned stand services running without OOM and an
empty due queue. Existing real Telegram test history remained intact.

Ignored evidence: `.cache/shuttle-live-test/native-task-closure-checkpoint.json`,
the two retained first-observation files, `task-closure-post-approval-input.json`,
`task-closure-before.dump` and `task-closure-runtime-final.json`. Source freezes,
test enumeration, per-run exit/OOM states and lint/build logs are under
`.cache/shuttle-work`. These artifacts can contain local fixture identifiers and
are not release artifacts.

The stand used manually authored provider mailbox responses, a synthetic task
and synthetic delivery evidence. This acceptance sent no Telegram message and
does not prove an unattended model's judgment, actual uncertain Telegram delivery,
production rollout, competing Loom-controller safety or a fix for the separate
River fast-start/stop lease observation.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
