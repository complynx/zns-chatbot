# Go platform: state at the development stop

This is a frozen documentation snapshot dated 10 October 2026, not a live task
board or permission to resume work. Daniel explicitly stopped development and
all agents. Only documentation consolidation continued. Integration commit:
`dd47f0e8fac3247db8de3d803b600b29c02953ba`, branch
`feature/go-platform-sandbox`. The morning deadline was missed.

The full objective remains Python-to-Go/PostgreSQL parity, architecture C–E,
independent Code and Functional acceptance, removable forward import, and final
real integrations. The objective is incomplete. Production is NO-GO. There is no
supported delivery ETA or implementation percentage.

## What exists

The repository contains a substantial Go implementation. These are source and
architecture facts; they do not establish complete behavioral acceptance.

| Area | Existing implementation and documentation |
| --- | --- |
| Main application | `platform/cmd/zns`, authenticated Core API, Telegram polling, Mini App and model adapter; [runtime](single-process-runtime.md), [configuration](configuration.md). |
| Domain services | Orders, meal/activity orders, passes/registration, massage, payments/proofs, exports, notifications and broadcast operations; [parity inventory](parity-current.md), [event contract](event-parity-contract.md), [broadcast contract](broadcast-parity.md). |
| Durable work | PostgreSQL state, inbox/receipts, delivery retries and operation projections, persisted conversation/workflow history; [inbox](telegram-inbox.md), [delivery](bot-delivery.md), [history](conversation-history.md). |
| Agent and knowledge | Typed tools, scoped authority, private/shared knowledge, proposal publication boundaries and sandboxed JavaScript; [scripting](agent-scripting.md), [script worker](script-worker.md), [ownership](architecture-ownership.md). |
| Isolated helpers | `scriptservice`, `scriptworker`, `mediaworker`, `stickerworker` and `znsreplace` executable sources; [media](audio-video-intake.md), [sticker worker](sticker-worker.md). The target remains one main CPU-only Linux application plus PostgreSQL and bounded helpers. |
| Identity | Telegram mapping, signed actor/session boundaries and Zitadel adapters; [identity](zitadel-identity.md), [runtime integration](zitadel-runtime.md). Source existence does not accept real IdP provisioning or final integration. |
| Import | Forward staging/planning/apply/reconciliation and importer-free runtime contracts; [one-off import](import-oneoff.md), [identity import](identity-import.md). Current full imported UI and recovery acceptance remain open. |
| Tests and tools | Real PostgreSQL tests, pinned Go/JS quality tooling, browser tests, a Telegram-like UI and FQA toolkit; [developer guide](go-platform-developer-guide.md), [QA guide](go-platform-qa-guide.md). |

Recent integrated changes include recipient/history permission fixes in
`33321d4`, and browser channel selection/Compose formatting in `dd47f0e`.
Affected checks and independent Code reviews exist. D004–D006 behavioral
PostgreSQL test changes are integrated. None of these changes independently
accepts its entire Functional domain.

## What is actually accepted

**Five of 38 active Functional scenarios have scoped acceptance:** C 1/15,
D 1/16, E 3/7. Thirty-three remain. This denominator is an acceptance matrix,
not a measure of source implementation or the entire final migration scope.

| Scenario | Accepted scope | Retained report |
| --- | --- | --- |
| F03 | Original fixture-backed locale/question-language behavior, old-card callbacks and fresh-session persistence; EN/RU × mouse/emulated touch. App/media lineage `6d01598e`, coordinator `db715972`; not full current composition or real-provider acceptance. | [Functional](qa/go-platform-pause-2026-10-10/f03-functional.md) |
| R01 | Current `665e689` Telegram-like messages/buttons/ACK/edits, manual/fixture-agent flows, upload/download, foreign-access denial and persistence in all four cells. | [Functional](qa/go-platform-pause-2026-10-10/r01-functional.md) |
| I01 | Original immutable inputs and attested owner mappings in the declared generations. Changed-input/partial-resume scope is not accepted. | [Functional](qa/go-platform-pause-2026-10-10/i01-functional.md) |
| I06 | Original isolated-target/role/session guards and sole bulk writer. Historical fresh targets did not prove current live composition. | [Functional](qa/go-platform-pause-2026-10-10/i06-functional.md) |
| I08 | Scoped removal of temporary receipts with permanent state preserved in the declared clone. Not complete imported runtime or final recovery. | [Functional](qa/go-platform-pause-2026-10-10/i08-functional.md) |

Reports retain their exact commit/image bindings, failures and provenance limits.
Unchanged source alone cannot transfer an old verdict to a new composition.

## What failed or remains unproven

### F05: private knowledge

Epoch4 reached genuine READY and business ACK, then an HTTP-response wait timed
out. No complete Functional cell was accepted. Accepted-input completion remains
uncertain; ACK is not evidence that the business action completed safely.
The app log cannot correlate the failed step sufficiently to prove a product
defect. All 168 native frames closed; the owned operator exited1 and was observed
absent. Physical closure does not clear input uncertainty or authorize replay.

The action instrument could mask an earlier fixture-hook error. QA-owned helper
successor5 preserves the original error and adds safe step correlation. Independent
Code1014 accepted its source conditionally; runtime is unrun. The next C stand5
was only a partial, unsealed source draft when stopped. Do not treat it as ready.
See [whole Code result](qa/go-platform-pause-2026-10-10/f05-code-whole.md),
[Functional failure](qa/go-platform-pause-2026-10-10/f05-functional-recovered.md)
and [helper source review](qa/go-platform-pause-2026-10-10/f05-helper-source-code.md).

### R12: recipient denial and durable batch progress

Build2 genuinely produced six binaries: `zns`, `znsreplace`, `scriptservice`,
`scriptworker`, `mediaworker`, `stickerworker`. Packaging then failed because
Created container Config adds `Cmd:null` and `Volumes:null` where image Config
omits those keys. All 214 native frames closed, resources were removed, and the
operator exited1. The whole attempt remains FAIL.

Independent Code1012 established suitability of 40 exact output pins for a
reviewed packaging-only successor. Install5 source was accepted by Code1015:
it reuses immutable binaries/tars, normalizes only those empty defaults and
retains all security checks. ROOT issued a narrow compiler-input receipt, but
no packaging3 runtime grant was issued. Seven new packaged component images,
artifact binding, composition, all four R12 cells and replacement proof remain
unrun/unaccepted. Do not borrow old leaf images or label the failed result PASS.
See [compiler review](qa/go-platform-pause-2026-10-10/r12-compiler-code.md) and
[packaging source review](qa/go-platform-pause-2026-10-10/r12-package-source-code.md).

### Massage timetable browser gate

Attempts1–5 did not execute the full original browser test. Failures exposed
transport, timestamp decoding, network allocation and Docker default-profile
representation issues. They remain failures, not removed or passed cases.
Attempt5 failed before CREATE because the pinned Linux Docker CLI path was
temporarily unavailable. Its two native frames closed; only its passive operator
was interrupted under explicit permission. The original cleanup deadline failed.

Later ROOT read-only observations found the same CLI digest and daemon, an empty
attempt5 namespace and no running containers. This is late current reconciliation,
not timely original-release PASS. The transient cause is not proven. Successor6
adds readiness polling inside the existing native15 operation; it is sealed
author source only. Code1016 was queued and interrupted at the stop; no sealed
acceptance report was present. Go180/Node120 and four screenshots remain unproven.

### E and final scope

Full E follows full accepted and released C. Imported identities/history/proof
bytes, owner/foreign UI access, importer-free runtime continuity, controlled
restart, whole-target recovery and final Before/After protected-state observations
remain mandatory. Historical E UI lifecycle failures remain open; see the [current runtime failure](qa/go-platform-pause-2026-10-10/e-current-runtime-functional.md). I03–I05 were
explicitly superseded by whole-target recovery; they are NOT PASS.

The full quality suite, real-provider/Telegram/Zitadel/payment test boundaries,
backup/restore/rollback and complete final composition are not accepted. Historical
suite inventory reported 65 packages and 2466 cases, including ten live cases;
re-enumerate from the resumed exact source. Genuine SKIP is never PASS.

## Missing compatibility surfaces and explicit exclusions

The dated [parity inventory](parity-current.md) identifies dedicated `/start`
welcome behavior, old `passes|…` callbacks and legacy browser route shapes
`/orders`, `/massage_timetable_data`, `/bot_name`, `/error` as remaining slices.
These are entry-protocol gaps, not missing entire domain services. Reconcile them
against resumed source and any explicit retirement decision before implementation.
They follow the approved C–E sequence; deferral does not remove them.

Avatar execution and its command/job runtime are excluded by the original scope.
Historical metadata and the typed external-job boundary remain. Unrestricted
MongoDB selectors and executable Python/Tornado templates are not required;
approved Go templates and typed/paged audience access replace them. Do not add
speculative SIGKILL matrices or generic recovery machinery as mandatory scope.

## Stop state and preserved local knowledge

Running sub-agent work was interrupted and completed workers were not restarted.
The old `d_r12_functional_current` handle still displayed `pending_init` after
STOP delivery and repeated interruption; no active operator was reported, and the
coordination API did not expose a handle-deletion operation. No further development,
test or resource dispatch was authorized after Daniel's stop. Heartbeat `go` was
verified PAUSED. The goal service returned no active goal when pause was requested;
this does not mean the project was completed or permission to continue exists.

Legacy Windows operator PID99216, birth `2026-10-09T08:49:06.8912258Z`, was still
present with UNKNOWN custody. It was not killed or treated as released. E14
protected data remain HOLD. Do not reset, reimport, prune or delete retained data.
Process numbers and resource bindings are historical identifiers; re-observe them
before any later action.

Ignored `management.local`, `qa.local` and worktree evidence contain detailed
operations, inputs and raw captures. They were not staged, deleted or rewritten
to improve verdicts. A clean checkout receives the
[retained knowledge archive](qa/go-platform-pause-2026-10-10/README.md), not live
stand access or private raw evidence. It contains selected exact public reports,
requirements and planning maps, with source paths and SHA256 provenance.

## What must happen after an explicit resume

Frozen local continuation inputs are retained at these relative paths. They may
be absent from a clean checkout; the archive retains their public request and
review knowledge. Do not execute an author packet without its required review
and a fresh actual resource/access binding.

| Input | Local path | Frozen identity/status |
| --- | --- | --- |
| C action helper5 | `qa.local/functional-f05-current-20261010/successor5` | SOURCE-SEAL `e5cd222687828b3f65dd2d276a3531a1e47617acff231da7eed18ada9206e5f1`; Code1014 seal `e6665ec579e047112fc04d5718c4b52faddc1db3c76c45c13326ad2650bbd9b4`, conditional source clean; runtime unrun. |
| C next stand | `qa.local/c-knowledge-f05-current-20261010/stand-successor5` | Partial unsealed draft only; no current grant, stand or Functional evidence. |
| D packaging-only install5 | `.worktrees/d002-public-recipient-selection/qa.local/d-r12-current-20261010/install5` | MANIFEST `ab37e410a730db559ec459568ef1da2ded7539be565b87225b53a4a153e01328`; Code1015 seal `054bb4decd82e915cb6f99b1be2c2f0b298d41b1281fc9d15da301b3188690aa`; ROOT compiler-scope receipt `a0f13824bda487b033b07435590beca3b9856e3709066c5399ac4e61e774317b`; no runtime authority. |
| D compiler outputs | `.worktrees/d002-public-recipient-selection/qa.local/d-r12-current-20261010/build-evidence2` | Failed whole RESULT `523973ecadaa73d6671263627cc905cc41a1c2ebe0bf5a22de9ca06500186f6e`; Code1012 actual seal `49af9c24526286308cffb883d3fd5bd79006cdc73a2ec0dea74358e4b31eb879`; 40 output pins permit conditional reuse only. |
| Timetable successor6 | `.worktrees/browser-test-channel-portability/qa.local/timetable-browser-dd47f0e-20261010/successor6` | MANIFEST `08a55fcd30b5012f12ece708bacb16c6510741095aeaacb86258edeae3570aea`, 283 pins; author seal `f5e4203ecf21fed2ea21e5d8e00c7dfcd2bb0c1f5e4717a0d098696bcb67a78b`; independent1016 not accepted. |

1. Re-observe processes, sessions, Docker identity, owned namespaces and E14
   data custody. Preserve unknown handles; old authority/START receipts are expired.
2. Use accepted compiler inputs and reviewed install5 to package all seven
   current R12 images under a fresh bounded grant. Complete actual composition
   before handing it to a new implementation-blind Functional reviewer.
3. Finish and review the coherent C stand using accepted helper5; perform the
   original F05 all-four-cell flow with safe input boundaries. Then continue
   remaining C, including F06/F07, registration ordering/retention and failure paths.
4. Review timetable successor6 and run the original Go/browser gate in its
   assigned bounds. Developer browser proof does not replace Functional QA.
5. Complete D and E, remaining parity slices and all final gates. Keep Code and
   Functional acceptance independent. Re-estimate only from completed outcomes.

Written by root (model not exposed/Codex)
on behalf of Daniel Drizhuk
