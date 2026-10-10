# Go platform developer guide

Development was explicitly stopped on 10 October 2026. This guide records how to
resume after Daniel authorizes it; it is not a launch instruction during the stop.
Read [the frozen state](go-platform-state-2026-10-10.md), `AGENTS.md`,
[migration requirements](go-migration.md) and [architecture scope](architecture-refactor-plan.md).

## Locate the responsible layer

| Change | Start here |
| --- | --- |
| Main composition/configuration/lifecycle | `platform/cmd/zns`, `platform/cmd/znsreplace`; [single-process runtime](single-process-runtime.md), [production contract](production-runtime-contract.md). |
| Telegram/manual/agent UI | `platform/internal/bot`, `platform/internal/telegram`, `platform/internal/miniapp`; [Telegram Markdown](telegram-markdown.md), [browser auth](browser-auth.md). |
| Orders/registration/massage/food/broadcast | Their domain packages under `platform/internal`; use [ownership](architecture-ownership.md) and the corresponding domain contracts. Keep authorization and transactions at the owning service. |
| History/private knowledge/agent tools | [Conversation history](conversation-history.md), [agent scripting](agent-scripting.md), [script API parity](script-api-parity-plan.md). A pending form is a hint, never a trap for the next message. |
| Inbox/delivery/retries | [Telegram inbox](telegram-inbox.md), [bot delivery](bot-delivery.md), [broadcast projection](broadcast-profile-projection.md). Preserve durable identities and factual unknown-send state. |
| Isolated execution/media | `platform/cmd/scriptservice`, `scriptworker`, `mediaworker`, `stickerworker`; [script worker](script-worker.md), [audio/video](audio-video-intake.md), [sticker worker](sticker-worker.md). |
| Schema/import/identity | [One-off import](import-oneoff.md), [identity import](identity-import.md), [Zitadel runtime](zitadel-runtime.md). Never infer owners from names. |

Use existing services and typed tools. Reproduce and explain a failure before
editing product code. An instrument error, missing readiness or source inventory
mismatch does not establish a product defect. Compare the smallest responsible
repair with a simpler existing path before adding infrastructure.

## Source and review workflow

Follow [Git development](git-development.md): disjoint developer worktrees and
`codex/` branches, exact commit/base, focused gates, clean handoff, independent
Code review and serialized integration. Locally reviewed commits/merges were
authorized; push, publication and production require separate permission.
Existing dirty documents at the stop were preserved, not silently reverted or
included in a broad stage command.

A ready handoff contains owned files, exact commit/base, resolved integration
conflicts, tested source identity, commands/results, limitations and affected
interfaces. Substantive post-review edits require affected review. Authors cannot
accept their own work. The approved persistent Code pool may reuse verified
unchanged bodies; Functional review remains separate and implementation-blind.

## Toolchain and checks

`platform/go.mod` declares Go1.27.0. Retained Linux execution used Go1.27.1,
Node24.21.0 and Playwright1.62.1; obtain exact current pinned tools from the
repository configuration, lockfiles and accepted tool receipts. Do not silently
upgrade dependencies, disable linters or change product browser defaults to make
a local check pass. The portability change permits an explicit test browser
channel override; the actual timetable browser proof remains open.

Run checks on Linux Docker/WSL. Windows is a thin dispatch surface. From
`platform`, after assigning resources and synthetic databases:

```sh
npm ci
npm run quality
npm run test:changed -- <integration-base-commit> --plan
npm run test:changed -- <integration-base-commit>
```

`npm ci` needs an available package registry or a prepared immutable cache; it
is preparation, not an acceptance command. Discovery can compile/run package
initialization. Plan/resource ownership first. The complete gate is:

```sh
npm run quality:all
```

See [test tiers](test-tiers.md) for `test:fast`, `test:slow`, `test:all` and
affected-domain classification. Both tiers and full mandatory checks remain
required for final acceptance; a changed-only plan does not waive them.
`TEST_DATABASE_URL` and `TEST_CREDIT_UPGRADE_DATABASE_URL` require owned synthetic
PostgreSQL; credit upgrade uses a separate cluster. Never use a Functional stand
occupied by another writer. Developers own runner mechanics; QA owns classification
and affected coverage mappings. Deleted structural assertions are not PASS.
The full runner also requires `SANDBOX_URL` for the admitted integration stand;
read `platform/scripts/quality.mjs` for the complete environment and command chain.

## Runtime and evidence discipline

Before resources, declare owner, exact names/paths, source/image pins, readonly
inputs, sole output writer and stop conditions. ROOT coordinates the three Linux
heavy-check slots. Strict globalEMPTY controllers cannot overlap foreign cohorts
even when nominal capacity remains. Current authority is explicit and finite;
templates, old STARTs and historical permission files are not live admissions.

Retain one original clock through preparation, review waits, execution and
cleanup. Normal shutdown should finish near one second and must be graceful
within five seconds; forced termination or timeout is FAIL. Observe physical
completion rather than sleeping for the ceiling. No simultaneous old/new writers.
Unknown native state retains original handles; quiet output is not restart permission.

Use actual producer-to-consumer composition: literal argv, mounts, image defaults,
health requirements, identity, current roster and source inventory. Docker may
represent an optional default as absent/null/false. Normalize only a documented
field whose semantics are proven equivalent; retain strict type/security checks
and immutable raw inputs. Idle acknowledgement alone did not prove CLI readiness
in the failed timetable attempt; source successor6 addresses that within native15.

Store live progress/custody in ignored `management.local`. Keep stable contracts,
sanitized acceptance reports and provenance in Git. Do not publish Config/Env,
credentials, provider controls, raw private history or generated binaries.
Build once per exact source/configuration and reuse authenticated immutable
outputs; the R12 compiler review permits packaging-only input reuse, not image
or Functional acceptance.

Written by root (model not exposed/Codex)
on behalf of Daniel Drizhuk
